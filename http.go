package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
)

var indexPageTemplate = template.Must(template.New("index").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>TalkXTyper</title>
	</head>
	<body>
		<h1>TalkXTyper is running</h1>
		<p>HTTP API:</p>
		<ul>
			<li><a href="/context">Context</a></li>
			<li><a href="/describe-screen">Describe Screen</a></li>
			<li><a href="/nvim">nvim Remote</a></li>
			<li><a href="/history">History</a></li>
		</ul>
	</body>
	</html>
`))

var contextPageTemplate = template.Must(template.New("context").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>Context</title>
	</head>
	<body>
		<h1>Current Context</h1>
		<p>The context is sent alongside transcription to help with understanding the user's intent. It can include keywords or other relevant strings.</p>
		<pre>{{.}}</pre>
		<form method="POST" action="/context">
			<label for="context">Set Context:</label><br>
			<textarea id="context" name="context" rows="4" cols="50"></textarea><br><br>
			<input type="submit" value="Submit">
		</form>
	</body>
	</html>
`))

var nvimListPageTemplate = template.Must(template.New("nvimList").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>nvim Instances</title>
	</head>
	<body>
		<h1>nvim instances</h1>
		{{if .Error}}<pre><b>{{.Error}}</b></pre>{{end}}
		{{if .Instances}}
			<table border="1" cellpadding="5" cellspacing="0" style="border-collapse: collapse;">
				<tr>
					<th>PID</th>
					<th>Socket</th>
					<th>Active</th>
					<th></th>
				</tr>
				{{range .Instances}}
					<tr>
						<td>{{.Pid}}</td>
						<td><code>{{.SocketFile}}</code></td>
						<td>{{if .Active}}yes{{end}}</td>
						<td><a href="/nvim?pid={{.Pid}}">Open</a></td>
					</tr>
				{{end}}
			</table>
		{{else}}
			<p>No running nvim instances found</p>
		{{end}}
	</body>
	</html>
`))

var nvimPageTemplate = template.Must(template.New("nvim").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>nvim Context</title>
	</head>
	<body>
		<h1>nvim remote (PID {{.Pid}})</h1>
		<p>
			<a href="/nvim">&larr; All instances</a>
			&nbsp;|&nbsp;
			Auto refresh:
			{{if .Refresh}}
				<b>on</b> (<a href="/nvim?pid={{.Pid}}&view={{.View}}">turn off</a>)
			{{else}}
				<b>off</b> (<a href="/nvim?pid={{.Pid}}&view={{.View}}&refresh=on">turn on</a>)
			{{end}}
		</p>
		<dl>
			<dt><a href="/nvim?pid={{.Pid}}&view=insertion{{if .Refresh}}&refresh=on{{end}}">Insertion text</a>{{if eq .View "insertion"}} &mdash; current{{end}}</dt>
			<dd>Lines around the cursor with a <code>&lt;&lt;CURSOR&gt;&gt;</code> sigil. Insert mode only.</dd>

			<dt><a href="/nvim?pid={{.Pid}}&view=visible{{if .Refresh}}&refresh=on{{end}}">Visible text</a>{{if eq .View "visible"}} &mdash; current{{end}}</dt>
			<dd>Visible contents of every window in the current tab.</dd>

			<dt><a href="/nvim?pid={{.Pid}}&view=preview{{if .Refresh}}&refresh=on{{end}}">Prompt preview</a>{{if eq .View "preview"}} &mdash; current{{end}}</dt>
			<dd>The exact prompt fragment transcription would receive right now (mode-dispatched, wrapped).</dd>

			<dt><a href="/nvim?pid={{.Pid}}&view=lua{{if .Refresh}}&refresh=on{{end}}">Lua REPL</a>{{if eq .View "lua"}} &mdash; current{{end}}</dt>
			<dd>Run arbitrary Lua against this nvim. Debug tool, not used by transcription.</dd>
		</dl>
		<h2>{{.ViewLabel}}</h2>
		{{if eq .View "lua"}}
		<form>
			<input type="hidden" name="pid" value="{{.Pid}}">
			<input type="hidden" name="view" value="lua">
			{{if .Refresh}}<input type="hidden" name="refresh" value="on">{{end}}
			<label for="command">Lua expression:</label><br>
			<textarea id="command" name="command" style="min-height: 100px; width: 100%; box-sizing: border-box;">{{.Command}}</textarea><br><br>
			<input type="submit" value="Run">
		</form>
		{{end}}

		{{if .Error}}<pre><b>{{.Error}}</b></pre>{{end}}

		<pre>{{.Context}}</pre>
		<script>
			(function() {
				const urlParams = new URLSearchParams(window.location.search);
				const refresh = urlParams.get('refresh');
				console.log("have refresh param:", refresh);
				if (refresh) {
					function refreshPage() {
						if (!document.activeElement || (document.activeElement.tagName !== "TEXTAREA" && document.activeElement.tagName !== "INPUT")) {
							location.reload();
						}
					}
					setInterval(refreshPage, 1000);
				}
			})();
		</script>

	</body>
	</html>
`))

var historyPageTemplate = template.Must(template.New("history").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>History</title>
	</head>
	<body>
		<h1>History</h1>
		{{if .History}}
			<table border="1" cellpadding="5" cellspacing="0" style="border-collapse: collapse;">
				<tr>
					<th>UUID</th>
					<th>Original</th>
					<th>Modified</th>
					<th>Repair Prompt</th>
					<th>MP3 Recording</th>
				</tr>
				{{range .History}}
					<tr>
						<td>{{.UUID}}</td>
						<td><pre style="white-space: pre-wrap;">{{.Original}}</pre></td>
						<td><pre style="white-space: pre-wrap;">{{.Modified}}</pre></td>
						<td><pre style="max-height: 200px; overflow-y: auto;">{{.RepairPrompt}}</pre></td>
						<td>
							{{if .Mp3Recording}}
								<audio controls preload="none">
									<source src="/history/mp3?uuid={{.UUID}}" type="audio/mpeg">
								</audio>
							{{end}}
						</td>
					</tr>
				{{end}}
			</table>
		{{else}}
			<p>No history available</p>
		{{end}}
	</body>
	</html>
`))

func withCORS(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		handler(w, r)
	}
}

func startServer() {
	http.HandleFunc("/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		err := indexPageTemplate.Execute(w, nil)
		if err != nil {
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
		}
	}))

	http.HandleFunc("/context", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			err := r.ParseForm()
			if err != nil {
				http.Error(w, "Error parsing form", http.StatusInternalServerError)
				return
			}
			contextValue := r.FormValue("context")
			taskManager.SetContext(contextValue)
			fmt.Fprintf(w, "Context stored")
		} else if r.Method == http.MethodGet {
			contextValue := taskManager.GetContext()

			err := contextPageTemplate.Execute(w, contextValue)
			if err != nil {
				http.Error(w, "Error rendering template", http.StatusInternalServerError)
			}
		} else {
			http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
		}
	}))

	http.HandleFunc("/describe-screen", withCORS(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		description, err := describeScreen(ctx)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error describing screen: %v", err), http.StatusInternalServerError)
			return
		}

		fmt.Fprintf(w, "Screen description: %s", description)
	}))

	http.HandleFunc("/nvim", withCORS(func(w http.ResponseWriter, r *http.Request) {
		pid := r.URL.Query().Get("pid")

		if pid == "" {
			instances, listErr := ListNvimInstances()
			activePid := ActiveWindowPid()

			type instanceRow struct {
				Pid        string
				SocketFile string
				Active     bool
			}
			rows := make([]instanceRow, 0, len(instances))
			for _, inst := range instances {
				rows = append(rows, instanceRow{
					Pid:        inst.Pid,
					SocketFile: inst.SocketFile,
					Active:     activePid != "" && hasAncestor(inst.Pid, activePid),
				})
			}

			err := nvimListPageTemplate.Execute(w, map[string]any{
				"Instances": rows,
				"Error":     listErr,
			})
			if err != nil {
				http.Error(w, "Error rendering template", http.StatusInternalServerError)
			}
			return
		}

		command := r.URL.Query().Get("command")
		view := r.URL.Query().Get("view")
		refresh := r.URL.Query().Get("refresh") != ""

		nvimClient := NewNvimClient()
		if err := nvimClient.SetPid(pid); err != nil {
			http.Error(w, fmt.Sprintf("Error targeting nvim PID %s: %v", pid, err), http.StatusInternalServerError)
			return
		}

		var nvimContext string
		var nvimError error
		var viewLabel string

		switch view {
		case "lua":
			viewLabel = "Lua REPL"
			if command != "" {
				nvimContext, nvimError = nvimClient.RemoteExecuteLua(command)
			}
		case "visible":
			viewLabel = "Visible text (raw GetVisibleText)"
			nvimContext, nvimError = nvimClient.GetVisibleText()
		case "preview":
			viewLabel = "Prompt preview (what transcription would receive)"
			nvimContext, nvimError = nvimClient.BuildTranscriptionContext()
		default:
			view = "insertion"
			viewLabel = "Insertion text (insert mode only)"
			nvimContext, nvimError = nvimClient.GetInsertionText("<<CURSOR>>")
		}

		err := nvimPageTemplate.Execute(w, map[string]any{
			"Pid":       pid,
			"View":      view,
			"Refresh":   refresh,
			"Command":   command,
			"Context":   nvimContext,
			"Error":     nvimError,
			"ViewLabel": viewLabel,
		})

		if err != nil {
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
		}
	}))

	http.HandleFunc("/history", withCORS(func(w http.ResponseWriter, r *http.Request) {
		history := taskManager.GetHistory()

		err := historyPageTemplate.Execute(w, map[string]any{"History": history})
		if err != nil {
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
		}
	}))

	http.HandleFunc("/history/mp3", withCORS(func(w http.ResponseWriter, r *http.Request) {
		uuid := r.URL.Query().Get("uuid")

		history := taskManager.GetHistory()
		for _, result := range history {
			if result.UUID == uuid {
				w.Header().Set("Content-Type", "audio/mpeg")
				w.Write(result.Mp3Recording)
				return
			}
		}

		http.Error(w, "MP3 file not found", http.StatusNotFound)
	}))

	http.HandleFunc("/start-task", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// if r.Method != http.MethodPost {
		// 	http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		// 	return
		// }

		task := taskManager.StartNewTask()
		<-task.waitForCompletion

		result := task.GetResult()

		if result == nil {
			http.Error(w, "Task failed to complete", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}))

	http.HandleFunc("/stop-task", withCORS(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
			return
		}

		taskManager.StopRecording()
		log.Println("Task stopped from HTTP API")
		w.WriteHeader(http.StatusNoContent)
	}))

	fmt.Printf("Server is starting on http://%s\n", config.ListenAddress)
	err := http.ListenAndServe(config.ListenAddress, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
	}
}
