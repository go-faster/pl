package pl

import (
	"strings"
	"testing"
	"time"

	"github.com/go-faster/jx"
)

// klogNow is the fixed "current" time supplying the year klog omits.
var klogNow = time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)

func TestParseKLog(t *testing.T) {
	for _, tt := range []struct {
		name string
		line string
		want []string
		skip []string
	}{
		{
			name: "unstructured",
			line: `I0223 01:11:35.888571     903 cidrallocator.go:278] updated ClusterIP allocator for Service CIDR 10.96.0.0/12`,
			want: []string{
				"01:11:35.888", "I ",
				"updated ClusterIP allocator for Service CIDR 10.96.0.0/12",
				"thread.id=903", "(cidrallocator.go:278)",
			},
		},
		{
			name: "structured",
			line: `E0223 01:22:17.141045    1386 prober_manager.go:197] "Startup probe already exists for container" pod="kube-system/cilium-envoy-vxgzg" containerName="cilium-envoy"`,
			want: []string{
				"E ", "Startup probe already exists for container",
				"pod=kube-system/cilium-envoy-vxgzg", "containerName=cilium-envoy",
				"(prober_manager.go:197)",
			},
		},
		{
			name: "structured error",
			line: `E0222 12:52:43.002962    1601 log.go:32] "ContainerStatus from runtime service failed" err="rpc error: code = NotFound desc = not found" containerID="0ec4b2cd"`,
			want: []string{
				"ContainerStatus from runtime service failed",
				"rpc error: code = NotFound desc = not found",
				"containerID=0ec4b2cd",
			},
			// The error is rendered bare, not as a key=value field.
			skip: []string{"err="},
		},
		{
			name: "warning",
			line: `W0223 01:11:35.888571     903 cacher.go:855] cacher: 1 objects queued`,
			want: []string{"W ", "cacher: 1 objects queued"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, ok := parseKLog(tt.line, klogNow)
			if !ok {
				t.Fatal("expected line to parse as klog")
			}
			f := &Formatter{TimeFormat: defaultTimeFormat, Location: time.UTC}
			out, ok := f.render(m)
			if !ok {
				t.Fatal("expected line to be printed")
			}
			for _, want := range tt.want {
				if !strings.Contains(out, want) {
					t.Errorf("output %q missing %q", out, want)
				}
			}
			for _, skip := range tt.skip {
				if strings.Contains(out, skip) {
					t.Errorf("output %q should not contain %q", out, skip)
				}
			}
		})
	}
}

func TestParseKLog_Timestamp(t *testing.T) {
	m, ok := parseKLog(`I0223 01:11:35.888571     903 cidrallocator.go:278] hi`, klogNow)
	if !ok {
		t.Fatal("expected line to parse as klog")
	}
	ts, ok := parseTime(m[keyTime])
	if !ok {
		t.Fatal("expected timestamp")
	}
	// The year comes from now; the header carries the rest.
	want := time.Date(2024, time.February, 23, 1, 11, 35, 888571000, time.UTC)
	if !ts.Equal(want) {
		t.Errorf("timestamp = %s, want %s", ts, want)
	}
}

func TestParseKLog_NotKLog(t *testing.T) {
	for _, line := range []string{
		"",
		"hello world",
		"Invalid line",
		`I0223 01:11:35.888571     903 cidrallocator.go:278`,       // No ']'.
		`X0223 01:11:35.888571     903 cidrallocator.go:278] hi`,   // Unknown level.
		`I0299 99:99:99.888571     903 cidrallocator.go:278] hi`,   // Invalid time.
		`Interesting: [a note] about something entirely different`, // Not a timestamp.
		`I0223 01:11:35.888571     903 cidrallocator.go:278] `,     // Empty message.
		`I0223 01:11:35.888571     abc cidrallocator.go:278] hi`,   // Thread id is not a number.
		`I0223 01:11:35.888571     903 not a source location] hi`,  // Source is not file:line.
	} {
		if m, ok := parseKLog(line, klogNow); ok {
			t.Errorf("%q: parsed as klog: %v", line, m)
		}
	}
}

func FuzzParseKLog(f *testing.F) {
	for _, s := range []string{
		`I0223 01:11:35.888571     903 cidrallocator.go:278] updated ClusterIP allocator for Service CIDR 10.96.0.0/12`,
		`E0223 01:22:17.141045    1386 prober_manager.go:197] "Startup probe already exists for container" pod="kube-system/cilium-envoy-vxgzg"`,
		`E0222 12:52:43.002962    1601 log.go:32] "ContainerStatus from runtime service failed" err="rpc error: code = NotFound desc = not found"`,
		`I0223 01:11:35.888571] hi`,
		"hello world",
	} {
		f.Add(s)
	}
	formatter := &Formatter{}
	f.Fuzz(func(t *testing.T, line string) {
		m, ok := parseKLog(line, klogNow)
		if !ok {
			return
		}
		// Every value must be valid JSON, as the renderer assumes.
		for k, v := range m {
			if !jx.Valid(v) {
				t.Fatalf("key %q: invalid JSON value %q", k, v)
			}
		}
		if _, ok := formatter.render(m); !ok {
			t.Fatal("expected rendered line to be printed")
		}
	})
}
