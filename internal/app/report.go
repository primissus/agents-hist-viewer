package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func RenderPatternReportMarkdown(w io.Writer, report PatternReport) {
	if len(report.Skills) > 0 {
		fmt.Fprintln(w, "# Skill candidates")
		for i, c := range report.Skills {
			renderSkillCandidate(w, i+1, c)
		}
	}
	if len(report.Scripts) > 0 {
		fmt.Fprintln(w, "\n# Script candidates")
		for i, c := range report.Scripts {
			renderScriptCandidate(w, i+1, c)
		}
	}
}

func renderSkillCandidate(w io.Writer, num int, c SkillCandidate) {
	fmt.Fprintf(w, "\n## %d. %s\n", num, c.Name)
	fmt.Fprintf(w, "- Intent: %s\n- Sessions: %d  Projects: %d  Confidence: %.2f\n", c.Intent, c.Sessions, c.Projects, c.Confidence)
	if len(c.TriggerPhrases) > 0 {
		fmt.Fprintf(w, "- Triggers: %s\n", strings.Join(c.TriggerPhrases, "; "))
	}
	if len(c.Outline) > 0 {
		fmt.Fprintln(w, "- Outline:")
		for _, step := range c.Outline {
			fmt.Fprintf(w, "  - %s\n", step)
		}
	}
	renderSamples(w, c.Samples)
}

func renderScriptCandidate(w io.Writer, num int, c ScriptCandidate) {
	fmt.Fprintf(w, "\n## %d. %s\n", num, c.Name)
	fmt.Fprintf(w, "- Purpose: %s\n- Sessions: %d\n", c.Purpose, c.Sessions)
	if len(c.NGram) > 0 {
		fmt.Fprintf(w, "- Sequence: %s\n", strings.Join(c.NGram, " → "))
	}
	if c.Script != "" {
		fmt.Fprintf(w, "- Proposed script:\n```bash\n%s\n```\n", c.Script)
	}
	if len(c.Params) > 0 {
		fmt.Fprintf(w, "- Params: %s\n", strings.Join(c.Params, ", "))
	}
	if c.RiskNotes != "" {
		fmt.Fprintf(w, "- Risk notes: %s\n", c.RiskNotes)
	}
	renderSamples(w, c.Samples)
}

func renderSamples(w io.Writer, samples []string) {
	if len(samples) == 0 {
		return
	}
	fmt.Fprintln(w, "- Samples:")
	for _, s := range samples {
		fmt.Fprintf(w, "  - %s\n", firstLine(s))
	}
}

// WritePatternReportFiles writes one SKILL-CANDIDATE-<slug>.md or
// SCRIPT-CANDIDATE-<slug>.md file per candidate into dir.
func WritePatternReportFiles(dir string, report PatternReport) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for _, c := range report.Skills {
		var b strings.Builder
		renderSkillCandidate(&b, 1, c)
		path := filepath.Join(dir, "SKILL-CANDIDATE-"+slugify(c.Name)+".md")
		if err := os.WriteFile(path, []byte(strings.TrimLeft(b.String(), "\n")), 0644); err != nil {
			return err
		}
	}
	for _, c := range report.Scripts {
		var b strings.Builder
		renderScriptCandidate(&b, 1, c)
		path := filepath.Join(dir, "SCRIPT-CANDIDATE-"+slugify(c.Name)+".md")
		if err := os.WriteFile(path, []byte(strings.TrimLeft(b.String(), "\n")), 0644); err != nil {
			return err
		}
	}
	return nil
}

func RenderPatternReportJSON(w io.Writer, report PatternReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "candidate"
	}
	return out
}
