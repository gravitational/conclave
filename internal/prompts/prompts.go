package prompts

import (
	"bytes"
	_ "embed"
	"text/template"
)

//go:embed plan.txt
var Plan string

//go:embed plan-refine.txt
var PlanRefine string

//go:embed assess.txt
var Assess string

//go:embed assess-dynamic.txt
var AssessDynamic string

//go:embed convene-steelman.txt
var ConveneSteelMan string

//go:embed convene-critique.txt
var ConveneCritique string

//go:embed convene-judge.txt
var ConveneJudge string

//go:embed convene-synthesis.txt
var ConveneSynthesis string

//go:embed complete.txt
var Complete string

// Render fills named template variables into a prompt string.
// Fields are referenced with {{.FieldName}} syntax in the text files.
func Render(tmpl string, data any) string {
	t := template.Must(template.New("").Parse(tmpl))
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		panic("prompt template error: " + err.Error())
	}
	return buf.String()
}
