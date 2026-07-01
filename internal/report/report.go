package report

import (
	"fmt"
	"time"
)

// Summary 测试摘要
type Summary struct {
	Date        string `json:"date"`
	Commit      string `json:"commit"`
	Config      string `json:"config"`
	Scenario    string `json:"scenario"`
	DurationSec int    `json:"duration_sec"`
	Passed      bool   `json:"passed"`
	Failures    []string `json:"failures"`
	OCPPSummary []string `json:"ocpp_summary"`
	MeterStats  map[string]float64 `json:"meter_stats"`
}

// WriteMarkdown 生成 Markdown 报告
func WriteMarkdown(s Summary) string {
	md := fmt.Sprintf("# AC Charger Simulator Test Report\n\n")
	md += fmt.Sprintf("- **Date**: %s\n", s.Date)
	md += fmt.Sprintf("- **Commit**: %s\n", s.Commit)
	md += fmt.Sprintf("- **Config**: %s\n", s.Config)
	md += fmt.Sprintf("- **Scenario**: %s\n", s.Scenario)
	md += fmt.Sprintf("- **Duration**: %ds\n", s.DurationSec)
	md += fmt.Sprintf("- **Result**: %s\n\n", passFail(s.Passed))
	
	if len(s.Failures) > 0 {
		md += "## Failures\n\n"
		for _, f := range s.Failures {
			md += fmt.Sprintf("- %s\n", f)
		}
		md += "\n"
	}
	
	md += "## OCPP Summary\n\n"
	for _, line := range s.OCPPSummary {
		md += fmt.Sprintf("- %s\n", line)
	}
	md += "\n"
	
	md += "## Meter Stats\n\n"
	for k, v := range s.MeterStats {
		md += fmt.Sprintf("- %s: %.3f\n", k, v)
	}
	md += "\n"
	
	return md
}

func passFail(passed bool) string {
	if passed {
		return "PASSED"
	}
	return "FAILED"
}

// NewSummary 创建默认摘要
func NewSummary(scenario, configPath string) Summary {
	return Summary{
		Date:     time.Now().Format(time.RFC3339),
		Config:   configPath,
		Scenario: scenario,
		Passed:   true,
	}
}
