package ai

type ReportGenerator interface {
	GenerateReport(prompt string) (string, error)
}
