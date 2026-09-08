package testutil

import "testing"

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
