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
			<li><a href="/nvim">Show nvim context</a></li>
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
		<h1>nvim Context</h1>
		<form>
			<label for="command">Enter Lua command:</label><br>
			<textarea id="command" name="command" style="min-height: 100px;">{{.Command}}</textarea><br><br>
			<input type="submit" value="Submit">
		</form>

		{{if .Error}}<pre><b>{{.Error}}</b></pre>{{end}}

		<pre>{{.Context}}</pre>
		<script>
			(function() {
				function refreshPage() {
					if (!document.activeElement || (document.activeElement.tagName !== "TEXTAREA" && document.activeElement.tagName !== "INPUT")) {
						location.reload();
					}
				}
				setInterval(refreshPage, 1000);
			})();
		</script>

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

	fmt.Printf("Server is starting on %s\n", config.ListenAddress)
	err := http.ListenAndServe(config.ListenAddress, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting server: %v\n", err)
	}
}
