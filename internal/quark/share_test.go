package quark

import (
	"testing"
)

func TestParseShareLink(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantPwdID string
		wantCode  string
		wantErr   bool
	}{
		{
			name:      "with passcode",
			input:     "https://pan.quark.cn/s/abc123 提取码:ab12",
			wantPwdID: "abc123",
			wantCode:  "ab12",
		},
		{
			name:      "fullwidth colon",
			input:     "https://pan.quark.cn/s/xyz789 提取码：x9y8",
			wantPwdID: "xyz789",
			wantCode:  "x9y8",
		},
		{
			name:      "no passcode",
			input:     "https://pan.quark.cn/s/def456",
			wantPwdID: "def456",
			wantCode:  "",
		},
		{
			name:      "with hash route",
			input:     "https://pan.quark.cn/s/g123abc/#/list/share/xxx",
			wantPwdID: "g123abc",
			wantCode:  "",
		},
		{
			name:    "invalid link",
			input:   "https://example.com/no/share/here",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pwdID, code, err := ParseShareLink(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseShareLink err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if pwdID != tt.wantPwdID {
				t.Errorf("pwdID = %q, want %q", pwdID, tt.wantPwdID)
			}
			if code != tt.wantCode {
				t.Errorf("passcode = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

func TestSecureRandomInt(t *testing.T) {
	for i := 0; i < 100; i++ {
		n, err := randInt(100, 999)
		if err != nil {
			t.Fatalf("randInt failed: %v", err)
		}
		if n < 100 || n > 999 {
			t.Errorf("n = %d, should be in [100, 999]", n)
		}
	}
}

func TestSecureRandomInt_InvalidRange(t *testing.T) {
	_, err := randInt(100, 100)
	if err == nil {
		t.Error("min==max should error")
	}
	_, err = randInt(200, 100)
	if err == nil {
		t.Error("min>max should error")
	}
}
