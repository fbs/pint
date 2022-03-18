package checks_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"

	"github.com/cloudflare/pint/internal/checks"
)

func newSeriesCheck(uri string) checks.RuleChecker {
	return checks.NewSeriesCheck(simpleProm("prom", uri, time.Second*5, true))
}

func noMetricText(name, uri, metric, since string) string {
	return fmt.Sprintf(`prometheus %q at %s didn't have any series for %q metric in the last %s`, name, uri, metric, since)
}

func noMetricRRText(name, uri, metric, since string) string {
	return fmt.Sprintf(`prometheus %q at %s didn't have any series for %q metric in the last %s but found recording rule that generates it, pint will try to use source recording rule queries to validate selectors in this query but it might be less accurate`, name, uri, metric, since)
}

func noFilterMatchText(name, uri, metric, label, filter, since string) string {
	return fmt.Sprintf(`prometheus %q at %s has %q metric with %q label but there are no series matching %s in the last %s`, name, uri, metric, label, filter, since)
}

func noLabelKeyText(name, uri, metric, label, since string) string {
	return fmt.Sprintf(`prometheus %q at %s has %q metric but there are no series with %q label in the last %s`, name, uri, metric, label, since)
}

func noSeriesText(name, uri, metric, since string) string {
	return fmt.Sprintf(`prometheus %q at %s didn't have any series for %q metric in the last %s`, name, uri, metric, since)
}

func seriesDisappearedText(name, uri, metric, since string) string {
	return fmt.Sprintf(`prometheus %q at %s doesn't currently have %q, it was last present %s ago`, name, uri, metric, since)
}

func filterDisappeardText(name, uri, metric, filter, since string) string {
	return fmt.Sprintf(`prometheus %q at %s has %q metric but doesn't currently have series matching %s, such series was last present %s ago`, name, uri, metric, filter, since)
}

func filterSometimesText(name, uri, metric, filter, since string) string {
	return fmt.Sprintf(`metric %q with label %s is only sometimes present on prometheus %q at %s with average life span of %s`, metric, filter, name, uri, since)
}

func seriesSometimesText(name, uri, metric, since, avg string) string {
	return fmt.Sprintf(`metric %q is only sometimes present on prometheus %q at %s with average life span of %s in the last %s`, metric, name, uri, avg, since)
}

func alertPresent(metric, alertname string) string {
	return fmt.Sprintf("%s metric is generated by alerts and found alerting rule named %q", metric, alertname)
}

func alertMissing(metric, alertname string) string {
	return fmt.Sprintf("%s metric is generated by alerts but didn't found any rule named %q", metric, alertname)
}

func TestSeriesCheck(t *testing.T) {
	testCases := []checkTest{
		{
			description: "ignores rules with syntax errors",
			content:     "- record: foo\n  expr: sum(foo) without(\n",
			checker:     newSeriesCheck,
			problems:    noProblems,
		},
		{
			description: "bad response",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "foo",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorBadData("prom", uri, "bad_data: bad input data"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithBadData(),
				},
			},
		},
		{
			description: "bad uri",
			content:     "- record: foo\n  expr: sum(foo)\n",
			checker: func(s string) checks.RuleChecker {
				return checks.NewSeriesCheck(simpleProm("prom", "http://", time.Second*5, false))
			},
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "foo",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorUnableToRun(checks.SeriesCheckName, "prom", "http://", `Post "http:///api/v1/query": http: no Host in request URL`),
						Severity: checks.Warning,
					},
				}
			},
		},
		{
			description: "simple query",
			content:     "- record: foo\n  expr: sum(notfound)\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "notfound",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricText("prom", uri, "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{requireRangeQueryPath},
					resp:  respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "complex query",
			content:     "- record: foo\n  expr: sum(found_7 * on (job) sum(sum(notfound))) / found_7\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "notfound",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricText("prom", uri, "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: "count(notfound)"},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: "count(notfound)"},
					},
					resp: respondWithEmptyMatrix(),
				},
				{
					conds: []requestCondition{requireQueryPath, formCond{key: "query", value: "count(found_7)"}},
					resp:  respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "label_replace()",
			content: `
- alert: foo
  expr: |
    count(
      label_replace(
        node_filesystem_readonly{mountpoint!=""},
        "device",
        "$2",
        "device",
        "/dev/(mapper/luks-)?(sd[a-z])[0-9]"
      )
    ) by (device,instance) > 0
    and on (device, instance)
    label_replace(
      disk_info{type="sat",interface_speed!="6.0 Gb/s"},
      "device",
      "$1",
      "disk",
      "/dev/(sd[a-z])"
    )
  for: 5m
`,
			checker:  newSeriesCheck,
			problems: noProblems,
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(disk_info{interface_speed!="6.0 Gb/s",type="sat"})`},
					},
					resp: respondWithSingleInstantVector(),
				},
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(node_filesystem_readonly{mountpoint!=""})`},
					},
					resp: respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "offset",
			content:     "- record: foo\n  expr: node_filesystem_readonly{mountpoint!=\"\"} offset 5m\n",
			checker:     newSeriesCheck,
			problems:    noProblems,
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(node_filesystem_readonly{mountpoint!=""})`},
					},
					resp: respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "negative offset",
			content:     "- record: foo\n  expr: node_filesystem_readonly{mountpoint!=\"\"} offset -15m\n",
			checker:     newSeriesCheck,
			problems:    noProblems,
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(node_filesystem_readonly{mountpoint!=""})`},
					},
					resp: respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "#1 series present",
			content:     "- record: foo\n  expr: found > 0\n",
			checker:     newSeriesCheck,
			problems:    noProblems,
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "#1 query error",
			content:     "- record: foo\n  expr: found > 0\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorUnableToRun(checks.SeriesCheckName, "prom", uri, "server_error: server error: 500"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithInternalError(),
				},
			},
		},
		{
			description: "#2 series never present",
			content:     "- record: foo\n  expr: sum(notfound)\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "notfound",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricText("prom", uri, "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{requireRangeQueryPath},
					resp:  respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "#2 series never present but recording rule provides it correctly",
			content:     "- record: foo\n  expr: sum(foo:bar{job=\"xxx\"})\n",
			checker:     newSeriesCheck,
			entries:     mustParseContent("- record: foo:bar\n  expr: sum(foo:bar)\n"),
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "foo:bar",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricRRText("prom", uri, "foo:bar", "1w"),
						Severity: checks.Information,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(foo:bar{job="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(foo:bar)`},
					},
					resp: respondWithEmptyMatrix(),
				},
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(sum(foo:bar) AND on(job) label_replace(vector(1), "job", "xxx", "", ""))`},
					},
					resp: respondWithSingleInstantVector(),
				},
			},
		},
		{
			description: "#2 series never present but recording rule provides it without results",
			content:     "- record: foo\n  expr: sum(foo:bar{job=\"xxx\"})\n",
			checker:     newSeriesCheck,
			entries:     mustParseContent("- record: foo:bar\n  expr: sum(foo)\n"),
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "foo:bar",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricRRText("prom", uri, "foo:bar", "1w"),
						Severity: checks.Information,
					},
					{
						Fragment: "foo",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricText("prom", uri, "foo", "1w"),
						Severity: checks.Bug,
					},
					{
						Fragment: "foo:bar",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricText("prom", uri, "foo:bar", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(foo:bar{job="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(foo:bar)`},
					},
					resp: respondWithEmptyMatrix(),
				},
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(sum(foo) AND on(job) label_replace(vector(1), "job", "xxx", "", ""))`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(foo)`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(foo)`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "#2 {ALERTS=...} present",
			content:     "- record: foo\n  expr: count(ALERTS{alertname=\"myalert\"})\n",
			checker:     newSeriesCheck,
			entries:     mustParseContent("- alert: myalert\n  expr: sum(foo) == 0\n"),
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `ALERTS{alertname="myalert"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     alertPresent(`ALERTS{alertname="myalert"}`, "myalert"),
						Severity: checks.Information,
					},
				}
			},
		},
		{
			description: "#2 {ALERTS=...} missing",
			content:     "- record: foo\n  expr: count(ALERTS{alertname=\"myalert\"})\n",
			checker:     newSeriesCheck,
			entries:     mustParseContent("- alert: notmyalert\n  expr: sum(foo) == 0\n"),
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `ALERTS{alertname="myalert"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     alertMissing(`ALERTS{alertname="myalert"}`, "myalert"),
						Severity: checks.Bug,
					},
				}
			},
		},
		{
			description: "#2 series never present but recording rule provides it, query error",
			content:     "- record: foo\n  expr: sum(foo:bar{job=\"xxx\"})\n",
			checker:     newSeriesCheck,
			entries:     mustParseContent("- record: foo:bar\n  expr: sum(foo:bar)\n"),
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "foo:bar",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noMetricRRText("prom", uri, "foo:bar", "1w"),
						Severity: checks.Information,
					},
					{
						Fragment: `foo:bar{job="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorBadData("prom", uri, "bad_data: bad input data"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(foo:bar{job="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(foo:bar)`},
					},
					resp: respondWithEmptyMatrix(),
				},
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(sum(foo:bar) AND on(job) label_replace(vector(1), "job", "xxx", "", ""))`},
					},
					resp: respondWithBadData(),
				},
			},
		},
		{
			description: "#2 query error",
			content:     "- record: foo\n  expr: found > 0\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorUnableToRun(checks.SeriesCheckName, "prom", uri, "server_error: server error: 500"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{requireQueryPath},
					resp:  respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{requireRangeQueryPath},
					resp:  respondWithInternalError(),
				},
			},
		},
		{
			description: "#3 metric present, label missing",
			content:     "- record: foo\n  expr: sum(found{job=\"foo\", notfound=\"xxx\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{job="foo",notfound="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noLabelKeyText("prom", uri, "found", "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{job="foo",notfound="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{job=~".+"}) by (job)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"job": "xxx"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{notfound=~".+"}) by (notfound)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
			},
		},
		{
			description: "#3 metric present, label query error",
			content:     "- record: foo\n  expr: sum(found{notfound=\"xxx\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{notfound="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorUnableToRun(checks.SeriesCheckName, "prom", uri, "server_error: server error: 500"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{notfound="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{notfound=~".+"}) by (notfound)`},
					},
					resp: respondWithInternalError(),
				},
			},
		},
		{
			description: "#4 metric was present but disappeared",
			content:     "- record: foo\n  expr: sum(found{job=\"foo\", instance=\"bar\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     seriesDisappearedText("prom", uri, "found", "4d"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{instance="bar",job="foo"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-4).Add(time.Minute*-5),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{job=~".+"}) by (job)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"job": "foo"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-4).Add(time.Minute*-5),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{instance=~".+"}) by (instance)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"instance": "bar"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-4).Add(time.Minute*-5),
								time.Minute*5,
							),
						},
					},
				},
			},
		},
		{
			description: "#5 metric was present but not with label",
			content:     "- record: foo\n  expr: sum(found{notfound=\"notfound\", instance=~\".+\", not!=\"negative\", instance!~\"bad\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{instance!~"bad",instance=~".+",not!="negative",notfound="notfound"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noFilterMatchText("prom", uri, "found", "notfound", `{notfound="notfound"}`, "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{instance!~"bad",instance=~".+",not!="negative",notfound="notfound"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{instance=~".+"}) by (instance)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"instance": "bar"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{not=~".+"}) by (not)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"not": "yyy"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{notfound=~".+"}) by (notfound)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"notfound": "found"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{instance=~".+"})`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"instance": "bar"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{notfound="notfound"})`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "#5 label query error",
			content:     "- record: foo\n  expr: sum(found{error=\"xxx\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{error="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     checkErrorUnableToRun(checks.SeriesCheckName, "prom", uri, "server_error: server error: 500"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{error="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{error=~".+"}) by (error)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"error": "bar"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{error="xxx"})`},
					},
					resp: respondWithInternalError(),
				},
			},
		},
		{
			description: "#5 high churn labels",
			content:     "- record: foo\n  expr: sum(sometimes{churn=\"notfound\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `sometimes{churn="notfound"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noFilterMatchText("prom", uri, "sometimes", "churn", `{churn="notfound"}`, "1w") + `, "churn" looks like a high churn label`,
						Severity: checks.Warning,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(sometimes{churn="notfound"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(sometimes)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-7).Add(time.Hour),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-5).Add(time.Minute*10),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2).Add(time.Minute*20),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(sometimes{churn=~".+"}) by (churn)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"churn": "aaa"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-7).Add(time.Hour),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"churn": "bbb"},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-5).Add(time.Minute*10),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"churn": "ccc"},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2).Add(time.Minute*20),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(sometimes{churn="notfound"})`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "#6 metric was always present but label disappeared",
			content:     "- record: foo\n  expr: sum({__name__=\"found\", removed=\"xxx\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{removed="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     filterDisappeardText("prom", uri, `{__name__="found"}`, `{removed="xxx"}`, "5d16h"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count({__name__="found",removed="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count({__name__="found"})`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count({__name__="found",removed=~".+"}) by (removed)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"removed": "xxx"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-6).Add(time.Hour*8),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{removed="xxx"})`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-6).Add(time.Hour*8),
								time.Minute*5,
							),
						},
					},
				},
			},
		},
		{
			description: "#7 metric was always present but label only sometimes",
			content:     "- record: foo\n  expr: sum(found{sometimes=\"xxx\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{sometimes="xxx"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     filterSometimesText("prom", uri, `found`, `{sometimes="xxx"}`, "18h45m"),
						Severity: checks.Warning,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{sometimes="xxx"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found)`},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{sometimes=~".+"}) by (sometimes)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"sometimes": "aaa"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"sometimes": "bbb"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-4),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"sometimes": "xxx"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-6).Add(time.Hour*8),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"sometimes": "xxx"},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-4),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"sometimes": "xxx"},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{sometimes="xxx"})`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-6).Add(time.Hour*8),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-4),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2),
								time.Minute*5,
							),
						},
					},
				},
			},
		},
		{
			description: "#8 metric is sometimes present",
			content:     "- record: foo\n  expr: sum(sometimes{foo!=\"bar\"})\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `sometimes`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     seriesSometimesText("prom", uri, "sometimes", "1w", "35m"),
						Severity: checks.Warning,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(sometimes{foo!="bar"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(sometimes)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-7).Add(time.Hour),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-5).Add(time.Minute*10),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2).Add(time.Minute*20),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(sometimes{foo=~".+"}) by (foo)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"foo": "aaa"},
								time.Now().Add(time.Hour*24*-7),
								time.Now().Add(time.Hour*24*-7).Add(time.Hour),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"foo": "bbb"},
								time.Now().Add(time.Hour*24*-5),
								time.Now().Add(time.Hour*24*-5).Add(time.Minute*10),
								time.Minute*5,
							),
							generateSampleStream(
								map[string]string{"foo": "ccc"},
								time.Now().Add(time.Hour*24*-2),
								time.Now().Add(time.Hour*24*-2).Add(time.Minute*20),
								time.Minute*5,
							),
						},
					},
				},
			},
		},
		{
			description: "series found, label missing",
			content:     "- record: foo\n  expr: found{job=\"notfound\"}\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `found{job="notfound"}`,
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noFilterMatchText("prom", uri, "found", "job", `{job="notfound"}`, "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(found{job="notfound"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: "count(found)"},
					},
					resp: respondWithSingleRangeVector1W(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{job=~".+"}) by (job)`},
					},
					resp: matrixResponse{
						samples: []*model.SampleStream{
							generateSampleStream(
								map[string]string{"job": "found"},
								time.Now().Add(time.Hour*24*-7),
								time.Now(),
								time.Minute*5,
							),
						},
					},
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(found{job="notfound"})`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "series missing, label missing",
			content:     "- record: foo\n  expr: notfound{job=\"notfound\"}\n",
			checker:     newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: "notfound",
						Lines:    []int{2},
						Reporter: checks.SeriesCheckName,
						Text:     noSeriesText("prom", uri, "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(notfound{job="notfound"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: "count(notfound)"},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "series missing, {__name__=}",
			content: `
- record: foo
  expr: '{__name__="notfound", job="bar"}'
`,
			checker: newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `{__name__="notfound"}`,
						Lines:    []int{3},
						Reporter: checks.SeriesCheckName,
						Text:     noSeriesText("prom", uri, `{__name__="notfound"}`, "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count({__name__="notfound",job="bar"})`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count({__name__="notfound"})`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
		{
			description: "series missing but check disabled",
			content: `
# pint disable promql/series(notfound)
- record: foo
  expr: count(notfound) == 0
`,
			checker:  newSeriesCheck,
			problems: noProblems,
		},
		{
			description: "series missing but check disabled, labels",
			content: `
# pint disable promql/series(notfound)
- record: foo
  expr: count(notfound{job="foo"}) == 0
`,
			checker:  newSeriesCheck,
			problems: noProblems,
		},
		{
			description: "series missing but check disabled, negative labels",
			content: `
# pint disable promql/series(notfound)
- record: foo
  expr: count(notfound{job!="foo"}) == 0
`,
			checker:  newSeriesCheck,
			problems: noProblems,
		},
		{
			description: "series missing, disabled comment for labels",
			content: `
# pint disable promql/series(notfound{job="foo"})
- record: foo
  expr: count(notfound) == 0
`,
			checker: newSeriesCheck,
			problems: func(uri string) []checks.Problem {
				return []checks.Problem{
					{
						Fragment: `notfound`,
						Lines:    []int{4},
						Reporter: checks.SeriesCheckName,
						Text:     noSeriesText("prom", uri, "notfound", "1w"),
						Severity: checks.Bug,
					},
				}
			},
			mocks: []*prometheusMock{
				{
					conds: []requestCondition{
						requireQueryPath,
						formCond{key: "query", value: `count(notfound)`},
					},
					resp: respondWithEmptyVector(),
				},
				{
					conds: []requestCondition{
						requireRangeQueryPath,
						formCond{key: "query", value: `count(notfound)`},
					},
					resp: respondWithEmptyMatrix(),
				},
			},
		},
	}
	runTests(t, testCases)
}
