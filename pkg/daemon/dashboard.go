package daemon

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/testground/testground/pkg/api"
	"github.com/testground/testground/pkg/logging"
	"github.com/testground/testground/tmpl"
)

type Item struct {
	Id      string
	Title   string
	Series  string
	RootURL string
	Unit    string
	Tags    []string
}

func (d *Daemon) dashboardHandler(engine api.Engine) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logging.S().With("req_id", r.Header.Get("X-Request-ID"))

		log.Debugw("handle request", "command", "dashboard task")
		defer log.Debugw("request handled", "command", "dashboard task")

		taskId := r.URL.Query().Get("task_id")
		if taskId == "" {
			fmt.Fprintf(w, "url param `task_id` is missing")
			return
		}

		tsk, err := engine.GetTask(taskId)
		if err != nil {
			fmt.Fprintf(w, "Cannot get task")
			return
		}

		name := clean(tsk.Plan) + "-" + tsk.Case
		fmt.Printf("name: %s\n", name)
		measurements, err := d.mv.GetMeasurements(name)
		if err != nil {
			fmt.Fprintf(w, "Cannot get measurements")
			return
		}

		if measurements == nil {
			fmt.Fprintf(w, "No measurements for this test plan.")
			return
		}

		t := template.New("measurements.html")

		content, err := tmpl.HtmlTemplates.ReadFile("measurements.html")
		if err != nil {
			panic(fmt.Sprintf("cannot find template file: %s", err))
		}
		t, err = t.Parse(string(content))
		if err != nil {
			panic(fmt.Sprintf("cannot ParseFiles with tmpl/measurements: %s", err))
		}

		data := struct {
			Plan  string
			Items []Item
		}{
			tsk.Plan + ":" + tsk.Case,
			nil,
		}

		for i, m := range measurements {
			tags, err := d.mv.GetTags(m)
			if err != nil {
				fmt.Fprintf(w, "failed to get tags for measurement %s: %s", m, err)
				return
			}

			tagsWithValues, err := d.mv.GetTagsValues(tags)
			if err != nil {
				fmt.Fprintf(w, "failed to get tags values for measurement %s: %s", m, err)
				return
			}

			_, marshaledTags, _, err := d.mv.GetData(m, tags, tagsWithValues)
			if err != nil {
				fmt.Fprintf(w, "failed to get data for measurement %s: %s", m, err)
				return
			}

			split := strings.Split(m, ".")
			d := Item{
				Title:   split[2],
				Series:  m,
				Unit:    split[len(split)-2],
				Id:      fmt.Sprintf("measurement_%d", i),
				RootURL: engine.EnvConfig().Daemon.RootURL,
				Tags:    marshaledTags,
			}
			data.Items = append(data.Items, d)
		}

		err = t.Execute(w, data)
		if err != nil {
			panic(fmt.Sprintf("cannot execute template: %s", err))
		}
	}
}

func clean(name string) string {
	forbiddenChar := "/"

	name = strings.Replace(name, forbiddenChar, "-", -1)

	return name
}
