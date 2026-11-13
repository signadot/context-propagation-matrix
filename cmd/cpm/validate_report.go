package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/signadot/context-propagation-matrix/internal/annotations"
)

// githubEscape applies GitHub Actions' workflow-command escaping to a message
// (the part after "::"): without it a newline would end the command early.
func githubEscape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// githubProperty escapes a command property value (title, file), which also
// may not contain ":" or ",".
func githubProperty(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C").Replace(s)
}

// githubAnnotation writes a "::level file=..,title=..::message" workflow
// command, which GitHub renders as an annotation on the run and the pull
// request. level is notice, warning or error. It writes nothing unless gh is
// set (GITHUB_ACTIONS), so local runs stay plain.
func githubAnnotation(w io.Writer, gh bool, level, file, title, msg string) {
	if !gh {
		return
	}
	var props []string
	if file != "" {
		props = append(props, "file="+githubProperty(file))
	}
	if title != "" {
		props = append(props, "title="+githubProperty(title))
	}
	cmd := "::" + level
	if len(props) > 0 {
		cmd += " " + strings.Join(props, ",")
	}
	fmt.Fprintf(w, "%s::%s\n", cmd, githubEscape(msg))
}

// reportProblems prints the annotation problems found by annotations.Apply and
// returns the ones that fail validation: fatal problems always, and warnings
// when strict (notes and advisory warnings never). Under GitHub Actions each
// problem is also surfaced as an annotation at the matching level.
func reportProblems(w io.Writer, gh, strict bool, problems []annotations.Problem) (failures []string) {
	for _, p := range problems {
		fmt.Fprintln(w, "  "+p.String())
		level := "warning"
		switch {
		case p.Fatal || (strict && !p.Info && !p.Advisory):
			failures = append(failures, p.String())
			level = "error"
		case p.Info:
			level = "notice"
		}
		githubAnnotation(w, gh, level, p.File, "annotation "+p.Annotation, p.Message)
	}
	return failures
}

// reportUnexplained prints the failing cells that no annotation covers as a
// warning: a count and the cells by variant. It never fails validation, with
// or without -strict, because a new upstream regression (or a new variant)
// legitimately arrives unexplained and must not block publishing; the warning
// exists so that someone writes the explanation.
func reportUnexplained(w io.Writer, gh bool, unexplained []string) {
	if len(unexplained) == 0 {
		fmt.Fprintln(w, "annotations: every non-pass cell is explained")
		return
	}
	head := fmt.Sprintf("annotations: %d non-pass cell(s) have no annotation", len(unexplained))
	lines := unexplainedLines(unexplained, 20)
	fmt.Fprintf(w, "warn: %s:\n", head)
	for _, line := range lines {
		fmt.Fprintln(w, "  "+line)
	}
	githubAnnotation(w, gh, "warning", "", "failing cells without an annotation",
		head+":\n"+strings.Join(lines, "\n"))
}
