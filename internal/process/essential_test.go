package process

import "testing"

func TestIsEssentialName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"systemd", true},
		{"systemd-journald", true},
		{"kworker", true},
		{"kworker/0:0H-events", true},
		{"NetworkManager", true},
		{"nginx", false},
		{"apache2", false},
		{"sshd", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsEssentialName(tc.name); got != tc.want {
			t.Errorf("IsEssentialName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsEssentialPID1(t *testing.T) {
	i := &Info{PID: 1, Comm: "anything", Cmdline: "/sbin/init"}
	if !IsEssential(i) {
		t.Error("PID 1 must be essential")
	}
}

func TestIsEssentialKernelThread(t *testing.T) {
	i := &Info{PID: 2, Comm: "kthreadd", Cmdline: ""}
	if !IsEssential(i) {
		t.Error("kernel thread (empty cmdline) must be essential")
	}
}

func TestIsEssentialUserProcess(t *testing.T) {
	i := &Info{PID: 1234, Comm: "nginx", Cmdline: "/usr/sbin/nginx"}
	if IsEssential(i) {
		t.Error("nginx should not be essential")
	}
}
