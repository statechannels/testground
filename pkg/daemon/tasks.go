package daemon

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/mitchellh/mapstructure"
	"github.com/testground/testground/pkg/api"
	"github.com/testground/testground/pkg/logging"
	"github.com/testground/testground/pkg/rpc"
	"github.com/testground/testground/pkg/runner"
	"github.com/testground/testground/pkg/task"
)

func (d *Daemon) tasksHandler(engine api.Engine) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		tgw := rpc.NewOutputWriter(w, r)

		var req api.TasksRequest
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			tgw.WriteError("tasks json decode", "err", err.Error())
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		tasks, err := engine.Tasks(req)
		if err != nil {
			tgw.WriteError("tasks json decode", "err", err.Error())
			return
		}

		tgw.WriteResult(tasks)
	}
}

const (
	EmojiSuccess    string = "&#9989;"
	EmojiCanceled   string = "&#9898;"
	EmojiFailure    string = "&#10060;"
	EmojiInProgress string = "&#9203;"
	EmojiScheduled  string = "&#128338;"
)

func (d *Daemon) listTasksHandler(engine api.Engine) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		log := logging.S().With("req_id", r.Header.Get("X-Request-ID"))

		log.Debugw("handle request", "command", "list tasks")
		defer log.Debugw("request handled", "command", "list tasks")

		w.Header().Set("Content-Type", "text/html")

		before := time.Now().Add(-7 * 24 * time.Hour)
		req := api.TasksRequest{
			Types:  []task.Type{task.TypeBuild, task.TypeRun},
			States: []task.State{task.StateScheduled, task.StateProcessing, task.StateComplete},
			Before: &before,
		}

		tasks, err := engine.Tasks(req)
		if err != nil {
			fmt.Fprintf(w, "tasks json decode error: %s", err.Error())
			return
		}

		cr, _ := engine.RunnerByName("cluster:k8s")
		rr := cr.(*runner.ClusterK8sRunner)

		var allocatableCPUs, allocatableMemory int64
		if rr.Enabled() {
			allocatableCPUs, allocatableMemory, _ = rr.GetClusterCapacity()
		}

		data := struct {
			Tasks          []interface{}
			ClusterEnabled bool
			CPUs           string
			Memory         string
		}{
			nil,
			rr.Enabled(),
			fmt.Sprintf("%d", allocatableCPUs),
			humanize.Bytes(uint64(allocatableMemory)),
		}

		tf := "Mon Jan _2 15:04:05"

		for _, t := range tasks {
			currentTask := struct {
				ID        string
				Name      string
				Created   string
				Updated   string
				Took      string
				Outcomes  string
				Status    string
				Error     string
				Actions   string
				CreatedBy string
			}{
				t.ID,
				t.Name(),
				t.Created().Format(tf),
				t.State().Created.Format(tf),
				t.Took().String(),
				"",
				"",
				t.Error,
				"",
				t.RenderCreatedBy(),
			}

			switch t.State().State {
			case task.StateComplete:
				outcome := decodeOutcome(t)
				currentTask.Outcomes = outcome.Content
				switch outcome.Outcome {
				case task.OutcomeSuccess:
					currentTask.Status = EmojiSuccess
				case task.OutcomeFailure:
					currentTask.Status = EmojiFailure
				default:
					currentTask.Status = EmojiFailure
				}
			case task.StateCanceled:
				currentTask.Status = EmojiCanceled
			case task.StateProcessing:
				currentTask.Status = EmojiInProgress
				currentTask.Actions = fmt.Sprintf(`<a href=/kill?task_id=%s>kill</a><br/><a onclick="return confirm('Are you sure?');" href=/delete?task_id=%s>delete</a>`, t.ID, t.ID)
				currentTask.Took = ""
			case task.StateScheduled:
				currentTask.Status = EmojiScheduled
				currentTask.Took = ""
			}

			data.Tasks = append(data.Tasks, currentTask)
		}

		t := template.New("tasks.html").Funcs(template.FuncMap{"unescape": unescape})
		t, err = t.ParseFiles("tmpl/tasks.html")
		if err != nil {
			panic(fmt.Sprintf("cannot ParseFiles with tmpl/tasks: %s", err))
		}

		err = t.Execute(w, data)
		if err != nil {
			panic(fmt.Sprintf("cannot execute template: %s", err))
		}
	}
}

func (d *Daemon) redirect() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/tasks", 301)
	}
}

func decodeResult(result interface{}) *runner.Result {
	r := &runner.Result{}
	err := mapstructure.Decode(result, r)
	if err != nil {
		logging.S().Errorw("error while decoding result", "err", err)
	}
	return r
}

type TaskOutcome struct {
	Outcome task.Outcome `json:"outcome"`
	Content string       `json:"content"`
}

func decodeOutcome(t task.Task) TaskOutcome {
	switch t.Type {
	case task.TypeBuild:
		if t.Error == "" {
			return TaskOutcome{Outcome: task.OutcomeSuccess, Content: fmt.Sprintf("artifacts: %s", t.Result)}
		}
		return TaskOutcome{Outcome: task.OutcomeFailure, Content: ""}
	case task.TypeRun:
		result := decodeResult(t.Result)
		return TaskOutcome{Outcome: result.Outcome, Content: result.String()}
	default:
		logging.S().Errorw("Unknown task type", "type", t.Type)
		return TaskOutcome{Outcome: task.OutcomeFailure, Content: ""}
	}
}

func unescape(s string) template.HTML {
	return template.HTML(s)
}
