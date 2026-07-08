// Package quark implements the Quark pan protocol layer.
//
// Design principles:
//   - The protocol layer only wraps Quark APIs and decouples from business logic.
//   - The Doer interface abstracts HTTP transport for mock injection in unit tests.
//   - Strongly-typed structs replace map[string]interface{}.
//   - context.Context flows through every request.
//   - Sentinel errors plus errors.Is replace string matching.
package quark

import "net/url"

// domains groups the three Quark API hosts by responsibility. Each host
// serves a disjoint family of endpoints, so callers always pair an endpoint
// path with the host that owns it.
//
//   - pan:      account / member info
//   - drivePC:  the bulk of file/upload/download operations
//   - driveH:   share sharepage token / detail lookups
var domains = struct {
	pan     string
	drivePC string
	driveH  string
}{
	pan:     "https://pan.quark.cn",
	drivePC: "https://drive-pc.quark.cn",
	driveH:  "https://drive-h.quark.cn",
}

// apiPaths enumerates every Quark endpoint path the package talks to,
// grouped by feature area. Grouping keeps related paths visible together
// and avoids a flat wall of PathXxx identifiers.
//
// Hosts for each path follow the convention documented on domains above;
// the call sites choose the matching host.
var apiPaths = struct {
	account struct {
		info   string
		member string
	}
	file struct {
		sort        string
		create      string
		move        string
		copy        string
		rename      string
		delete      string
		download    string
		search      string
		unarchive   string
		previewTree string
	}
	upload struct {
		pre    string
		hash   string
		auth   string
		finish string
	}
	share struct {
		base            string
		password        string
		delete          string
		mypageDetail    string
		sharepageToken  string
		sharepageDetail string
		sharepageSave   string
	}
	task string
}{
	account: struct {
		info   string
		member string
	}{
		info:   "/account/info",
		member: "/1/clouddrive/member",
	},
	file: struct {
		sort        string
		create      string
		move        string
		copy        string
		rename      string
		delete      string
		download    string
		search      string
		unarchive   string
		previewTree string
	}{
		sort:        "/1/clouddrive/file/sort",
		create:      "/1/clouddrive/file",
		move:        "/1/clouddrive/file/move",
		copy:        "/1/clouddrive/file/copy",
		rename:      "/1/clouddrive/file/rename",
		delete:      "/1/clouddrive/file/delete",
		download:    "/1/clouddrive/file/download",
		search:      "/1/clouddrive/file/search",
		unarchive:   "/1/clouddrive/archive/unarchive",
		previewTree: "/1/clouddrive/archive/preview_tree",
	},
	upload: struct {
		pre    string
		hash   string
		auth   string
		finish string
	}{
		pre:    "/1/clouddrive/file/upload/pre",
		hash:   "/1/clouddrive/file/update/hash",
		auth:   "/1/clouddrive/file/upload/auth",
		finish: "/1/clouddrive/file/upload/finish",
	},
	share: struct {
		base            string
		password        string
		delete          string
		mypageDetail    string
		sharepageToken  string
		sharepageDetail string
		sharepageSave   string
	}{
		base:            "/1/clouddrive/share",
		password:        "/1/clouddrive/share/password",
		delete:          "/1/clouddrive/share/delete",
		mypageDetail:    "/1/clouddrive/share/mypage/detail",
		sharepageToken:  "/1/clouddrive/share/sharepage/token",
		sharepageDetail: "/1/clouddrive/share/sharepage/detail",
		sharepageSave:   "/1/clouddrive/share/sharepage/save",
	},
	task: "/1/clouddrive/task",
}

// defaultQueryParams returns the pr=ucpro&fr=pc pair that every regular
// Quark API request must carry. A fresh url.Values is returned each call
// so the caller can mutate it without affecting siblings.
func defaultQueryParams() url.Values {
	q := url.Values{}
	q.Set("pr", "ucpro")
	q.Set("fr", "pc")
	return q
}
