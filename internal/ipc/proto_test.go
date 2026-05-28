package ipc_test

import (
	"strings"
	"testing"

	"github.com/sgaunet/runq/internal/ipc"
)

func TestValidate_HelloAndStop(t *testing.T) {
	for _, kind := range []string{ipc.KindHello, ipc.KindStop} {
		req := ipc.Request{Version: ipc.Version, Kind: kind}
		code, err := req.Validate()
		if err != nil {
			t.Errorf("kind=%q: validate err = %v code=%q", kind, err, code)
		}
	}
}

func TestValidate_VersionMismatch(t *testing.T) {
	req := ipc.Request{Version: 999, Kind: ipc.KindHello}
	code, err := req.Validate()
	if err == nil {
		t.Fatal("expected error for version mismatch")
	}
	if code != ipc.CodeUnsupportedVersion {
		t.Errorf("code = %q, want %q", code, ipc.CodeUnsupportedVersion)
	}
}

func TestValidate_SubmitChecks(t *testing.T) {
	cases := []struct {
		name string
		req  ipc.Request
		code string
	}{
		{
			name: "empty commands",
			req:  ipc.Request{Version: ipc.Version, Kind: ipc.KindSubmit, Commands: nil},
			code: ipc.CodeBadRequest,
		},
		{
			name: "too many commands",
			req: ipc.Request{Version: ipc.Version, Kind: ipc.KindSubmit,
				Commands: makeNCommands(ipc.MaxCommandsPerSubmit + 1)},
			code: ipc.CodeTooManyCommands,
		},
		{
			name: "empty text",
			req: ipc.Request{Version: ipc.Version, Kind: ipc.KindSubmit,
				Commands: []ipc.SubmitItem{{Text: ""}}},
			code: ipc.CodeEmptyText,
		},
		{
			name: "invalid timeout",
			req: ipc.Request{Version: ipc.Version, Kind: ipc.KindSubmit,
				Commands: []ipc.SubmitItem{{Text: "echo hi", Timeout: "not-a-duration"}}},
			code: ipc.CodeInvalidTimeout,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := tc.req.Validate()
			if err == nil {
				t.Fatalf("expected error")
			}
			if code != tc.code {
				t.Errorf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

func TestValidate_UnknownKind(t *testing.T) {
	req := ipc.Request{Version: ipc.Version, Kind: "explode"}
	code, err := req.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	if code != ipc.CodeBadRequest {
		t.Errorf("code = %q, want %q", code, ipc.CodeBadRequest)
	}
}

func TestEncodeLine_RejectsTooLarge(t *testing.T) {
	huge := strings.Repeat("x", ipc.MaxMessageBytes)
	req := ipc.Request{
		Version:  ipc.Version,
		Kind:     ipc.KindSubmit,
		Commands: []ipc.SubmitItem{{Text: huge}},
	}
	_, err := ipc.EncodeLine(req)
	if err == nil {
		t.Errorf("expected too-large error")
	}
}

func TestEncodeLine_OK(t *testing.T) {
	req := ipc.Request{Version: ipc.Version, Kind: ipc.KindHello}
	got, err := ipc.EncodeLine(req)
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != '\n' {
		t.Errorf("missing trailing newline")
	}
}

func makeNCommands(n int) []ipc.SubmitItem {
	out := make([]ipc.SubmitItem, n)
	for i := range out {
		out[i] = ipc.SubmitItem{Text: "echo hi"}
	}
	return out
}
