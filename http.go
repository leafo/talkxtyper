package main

import (
	"fmt"
	"html/template"
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
			<li><a href="/start-recording">Start Recording</a></li>
			<li><a href="/stop-recording">Stop Recording</a></li>
			<li><a href="/abort-recording">Abort Recording</a></li>
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

var nvimPageTemplate = template.Must(template.New("nvim").Parse(`
	<!DOCTYPE html>
	<html>
	<head>
		<title>nvim Context</title>
	</head>
	<body>
		<h1>nvim remote</h1>
		<form>
			<label for="command">Enter Lua command:</label><br>
			<textarea id="command" name="command" style="min-height: 100px; width: 100%; box-sizing: border-box;">{{.Command}}</textarea><br><br>

			<div>
				<input type="submit" value="Submit">
				<button type="submit" name="refresh" value="on">Auto Refresh</button>
			</div>
		</form>

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
						<td>{{if .Mp3Recording}}<a href="/history/mp3?uuid={{.UUID}}">Recording</a>{{end}}</td>
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

	http.HandleFunc("/start-recording", withCORS(func(w http.ResponseWriter, r *http.Request) {
		taskManager.StartNewTask()
		fmt.Fprintf(w, "Recording started")
	}))

	http.HandleFunc("/stop-recording", withCORS(func(w http.ResponseWriter, r *http.Request) {
		// TODO: can we make a method to assign the task to the request so we can
		// get transcription result from the http api
		taskManager.StopRecording()
		fmt.Fprintf(w, "Recording stopped")
	}))

	http.HandleFunc("/abort-recording", withCORS(func(w http.ResponseWriter, r *http.Request) {
		taskManager.Abort()
		fmt.Fprintf(w, "Recording aborted")
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
		command := r.URL.Query().Get("command")

		nvimClient := NewNvimClient()
		err := nvimClient.FindFirstNvim()
		if err != nil {
			http.Error(w, fmt.Sprintf("Error finding active nvim instance: %v", err), http.StatusInternalServerError)
			return
		}

		var nvimContext string
		var nvimError error

		if command != "" {
			nvimContext, nvimError = nvimClient.RemoteExecuteLua(command)
		} else {
			nvimContext, nvimError = nvimClient.GetInsertionText("<<CURSOR>>")
		}

		err = nvimPageTemplate.Execute(w, map[string]interface{}{
			"Command": command,
			"Context": nvimContext,
			"Error":   nvimError,
		})

		if err != nil {
			http.Error(w, "Error rendering template", http.StatusInternalServerError)
		}
	}))

	http.HandleFunc("/history", withCORS(func(w http.ResponseWriter, r *http.Request) {
		history := taskManager.GetHistory()

		err := historyPageTemplate.Execute(w, map[string]interface{}{"History": history})
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

	fmt.Printf("Server is starting on http://%s\n", config.ListenAddress)
	err := http.ListenAndServe(config.ListenAddress, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
	}
}
