package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/phpgao/diskcli/internal/provider"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// treeOptions controls tree rendering and traversal.
type treeOptions struct {
	// maxDepth caps how many levels are expanded (0 = unlimited).
	maxDepth int
	// dirsOnly keeps only directories, mirroring `tree -d`.
	dirsOnly bool
	// humanReadable appends human-readable sizes to files, mirroring `tree -h`.
	humanReadable bool
	// concurrency is the max number of directories fetched in parallel.
	concurrency int
}

// newTreeCmd creates the tree subcommand, which prints a directory hierarchy
// in the style of the Unix `tree` utility.
func newTreeCmd() *cobra.Command {
	var opts treeOptions
	cmd := &cobra.Command{
		Use:   "tree [path]",
		Short: "Display directory tree",
		Long: `Display the directory hierarchy starting at path (default /), like the
Unix tree command. Directory traversal is concurrent (--concurrency) to speed
up large trees; output order stays stable (sorted by name).

Examples:
  diskcli tree
  diskcli tree /movies
  diskcli tree /docs -L 2 -d
  diskcli tree /photos -h
  diskcli tree /backup -C 10`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prov, err := mustProvider(cmd)
			if err != nil {
				return err
			}
			root := "/"
			if len(args) > 0 {
				root = args[0]
			}
			// fetch wires the provider's List into the renderer. listChildren
			// already paginates internally, so every call returns a full listing.
			fetch := func(ctx context.Context, p string) ([]*provider.FileItem, error) {
				res, lerr := prov.List(ctx, p, provider.ListOpts{PageSize: 100, All: true})
				if lerr != nil {
					return nil, lerr
				}
				return res.Items, nil
			}
			return renderTree(cmd.Context(), cmd.OutOrStdout(), root, fetch, opts)
		},
	}
	cmd.Flags().IntVarP(&opts.maxDepth, "max-depth", "L", 0, "max display depth (0=unlimited)")
	cmd.Flags().BoolVarP(&opts.dirsOnly, "dirs-only", "d", false, "list directories only")
	cmd.Flags().BoolVarP(&opts.humanReadable, "human-readable", "h", false, "print file sizes in human-readable form")
	cmd.Flags().IntVarP(&opts.concurrency, "concurrency", "C", 5, "directory traversal concurrency")
	// Reserve --help without the -h shorthand so -h can mean --human-readable,
	// matching the Unix tree command (help is still available via --help).
	cmd.Flags().Bool("help", false, "help for tree")
	return cmd
}

// treeNode is an in-memory snapshot of one entry and its already-fetched children.
type treeNode struct {
	item     *provider.FileItem
	children []*treeNode
}

// renderTree prints the root header followed by the rendered tree.
//
// The fetch callback is injected so the rendering logic is unit-testable with
// an in-memory source instead of a live provider. Traversal is concurrent
// (capped by a global semaphore); rendering is synchronous and sorted, so
// output order never depends on goroutine scheduling.
func renderTree(ctx context.Context, w io.Writer, root string, fetch func(ctx context.Context, p string) ([]*provider.FileItem, error), opts treeOptions) error {
	concurrency := opts.concurrency
	if concurrency < 1 {
		concurrency = 5
	}
	sem := semaphore.NewWeighted(int64(concurrency))

	header := strings.TrimRight(root, "/")
	if header == "" {
		header = "/"
	}
	_, _ = fmt.Fprintln(w, header)

	items, err := fetchThrottled(ctx, sem, fetch, root)
	if err != nil {
		return err
	}
	rootNode := &treeNode{item: &provider.FileItem{Name: header, Path: root, IsDirectory: true}}
	children, err := buildChildren(ctx, sem, fetch, items, opts, 0)
	if err != nil {
		return err
	}
	rootNode.children = children
	renderNode(w, rootNode, "", 0, opts)
	return nil
}

// fetchThrottled acquires a semaphore slot around the (network-bound) fetch so
// traversal concurrency is globally capped at opts.concurrency.
func fetchThrottled(ctx context.Context, sem *semaphore.Weighted, fetch func(ctx context.Context, p string) ([]*provider.FileItem, error), p string) ([]*provider.FileItem, error) {
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer sem.Release(1)
	return fetch(ctx, p)
}

// buildChildren fetches and builds the subtree for the given entries
// concurrently. Only directories are expanded; files become leaf nodes.
func buildChildren(ctx context.Context, sem *semaphore.Weighted, fetch func(ctx context.Context, p string) ([]*provider.FileItem, error), items []*provider.FileItem, opts treeOptions, depth int) ([]*treeNode, error) {
	// Respect -d: drop files so only directories remain.
	entries := make([]*provider.FileItem, 0, len(items))
	for _, it := range items {
		if opts.dirsOnly && !it.IsDirectory {
			continue
		}
		entries = append(entries, it)
	}

	nodes := make([]*treeNode, len(entries))
	// errgroup propagates the first error; the semaphore (not SetLimit) caps
	// actual concurrency, avoiding the hierarchical-deadlock pitfall of a
	// nested errgroup with SetLimit.
	g, gctx := errgroup.WithContext(ctx)
	for i, it := range entries {
		i, it := i, it
		g.Go(func() error {
			sub, e := buildNode(gctx, sem, fetch, it, opts, depth)
			if e != nil {
				return e
			}
			nodes[i] = sub
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// buildNode builds a single node and, when it is a directory within the depth
// cap, recursively builds its children.
func buildNode(ctx context.Context, sem *semaphore.Weighted, fetch func(ctx context.Context, p string) ([]*provider.FileItem, error), it *provider.FileItem, opts treeOptions, depth int) (*treeNode, error) {
	node := &treeNode{item: it}
	if !it.IsDirectory {
		return node, nil
	}
	// Stop descending once the depth cap is reached (mirrors -L semantics).
	if opts.maxDepth > 0 && depth+1 >= opts.maxDepth {
		return node, nil
	}
	items, err := fetchThrottled(ctx, sem, fetch, it.Path)
	if err != nil {
		return nil, err
	}
	children, err := buildChildren(ctx, sem, fetch, items, opts, depth+1)
	if err != nil {
		return nil, err
	}
	node.children = children
	return node, nil
}

// renderNode prints the tree synchronously and in a stable order (entries
// sorted by name), so concurrent fetching never affects output ordering.
func renderNode(w io.Writer, node *treeNode, prefix string, depth int, opts treeOptions) {
	entries := node.children
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].item.Name < entries[j].item.Name
	})
	for i, child := range entries {
		last := i == len(entries)-1
		connector := "├── "
		childPrefix := prefix + "│   "
		if last {
			connector = "└── "
			childPrefix = prefix + "    "
		}
		name := child.item.Name
		if child.item.IsDirectory {
			name += "/"
		}
		if opts.humanReadable && !child.item.IsDirectory {
			name += "  " + formatBytes(child.item.Size)
		}
		_, _ = fmt.Fprintf(w, "%s%s%s\n", prefix, connector, name)

		if child.item.IsDirectory && (opts.maxDepth == 0 || depth+1 < opts.maxDepth) {
			renderNode(w, child, childPrefix, depth+1, opts)
		}
	}
}
