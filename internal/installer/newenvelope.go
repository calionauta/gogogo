package installer

import (
	"encoding/json"
	"io"
)

// newEnvelope is the JSON an apply prints: what it created, what it edited,
// whether the proof build passed, and the next commands. It is the machine
// contract for a scaffold, so it lives beside its printer rather than inside
// the command file.
type newEnvelope struct {
	Name       string   `json:"name"`
	Dir        string   `json:"dir"`
	Created    []string `json:"created"`
	Edits      []string `json:"edits"`
	BuildOk    bool     `json:"buildOk"`
	BuildError string   `json:"buildError,omitempty"`
	Next       []string `json:"next"`
}

// printNewEnvelope writes the apply result. A proof failure is reported in
// the envelope AND returned, so both a human reading the transcript and an
// agent branching on the exit code see the same verdict.
//
// Split from newcmd.go with the envelope type it uses: the file gates at 500
// lines, and the JSON shape of an apply result is a self-contained unit — the
// next field added to it should not force a judgement call about what else in
// the scaffold command to move.
func printNewEnvelope(w io.Writer, opt newOptions, plan newPlan, applyErr error) {
	env := newEnvelope{
		Name:    opt.name,
		Dir:     opt.dir,
		Created: []string{plan.paths.handler, plan.paths.page, plan.paths.card},
		Edits:   []string{plan.paths.chi, plan.paths.registry, plan.paths.policy},
		BuildOk: applyErr == nil,
		Next:    newNextSteps(opt.name),
	}
	if applyErr != nil {
		env.BuildError = applyErr.Error()
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(env)
}
