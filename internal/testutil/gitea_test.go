package testutil

import "testing"

func TestParseLSRemoteRef(t *testing.T) {
	const ref = "refs/heads/feature/allowed"
	const oid = "0123456789abcdef0123456789abcdef01234567"
	for _, test := range []struct {
		name    string
		output  string
		want    string
		found   bool
		wantErr bool
	}{
		{name: "exact", output: oid + "\t" + ref + "\n", want: oid, found: true},
		{name: "empty"},
		{name: "different ref", output: oid + "\trefs/heads/main\n"},
		{name: "malformed fields", output: oid + " " + ref + "\n", wantErr: true},
		{name: "invalid oid", output: "not-an-object-id\t" + ref + "\n", wantErr: true},
		{name: "duplicate", output: oid + "\t" + ref + "\n" + oid + "\t" + ref + "\n", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, found, err := parseLSRemoteRef(test.output, ref)
			if (err != nil) != test.wantErr || got != test.want || found != test.found {
				t.Fatalf("parseLSRemoteRef() = %q, %t, %v; want %q, %t, error=%t", got, found, err, test.want, test.found, test.wantErr)
			}
		})
	}
}

func TestParseAgentPIDRequiresPositiveInteger(t *testing.T) {
	for name, test := range map[string]struct {
		output string
		want   int
		valid  bool
	}{
		"agent output": {output: "SSH_AUTH_SOCK=/tmp/agent.sock; export SSH_AUTH_SOCK;\nSSH_AGENT_PID=1234; export SSH_AGENT_PID;\n", want: 1234, valid: true},
		"missing":      {output: "SSH_AUTH_SOCK=/tmp/agent.sock; export SSH_AUTH_SOCK;"},
		"not integer":  {output: "SSH_AGENT_PID=process; export SSH_AGENT_PID;"},
		"zero":         {output: "SSH_AGENT_PID=0; export SSH_AGENT_PID;"},
		"negative":     {output: "SSH_AGENT_PID=-1; export SSH_AGENT_PID;"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := parseAgentPID(test.output)
			if test.valid {
				if err != nil || got != test.want {
					t.Fatalf("parseAgentPID() = %d, %v; want %d, nil", got, err, test.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseAgentPID() = %d, nil; want error", got)
			}
		})
	}
}
