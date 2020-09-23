package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/fiftin/semaphore/api/projects"
	"github.com/fiftin/semaphore/api/sockets"
	"github.com/fiftin/semaphore/api/tasks"

	"github.com/fiftin/semaphore/util"
	"github.com/gobuffalo/packr"
	"github.com/gorilla/mux"
	"github.com/russross/blackfriday"
)

var publicAssets = packr.NewBox("../web/public")

//JSONMiddleware ensures that all the routes respond with Json, this is added by default to all routes
func JSONMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		next.ServeHTTP(w, r)
	})
}

//plainTextMiddleware resets headers to Plain Text if needed
func plainTextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		next.ServeHTTP(w, r)
	})
}

func pongHandler(w http.ResponseWriter, r *http.Request) {
	//nolint: errcheck
	w.Write([]byte("pong"))
}

func notFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.WriteHeader(http.StatusNotFound)
	//nolint: errcheck
	w.Write([]byte("404 not found"))
	fmt.Println(r.Method, ":", r.URL.String(), "--> 404 Not Found")
}

// Route declares all routes
func Route() *mux.Router {
	r := mux.NewRouter().StrictSlash(true)
	r.NotFoundHandler = http.HandlerFunc(servePublic)

	webPath := "/"
	if util.WebHostURL != nil {
		r.Host(util.WebHostURL.Hostname())
		webPath = util.WebHostURL.Path
	}

	r.Use(mux.CORSMethodMiddleware(r))

	pingRouter := r.Path(webPath + "api/ping").Subrouter()
	pingRouter.Use(plainTextMiddleware)
	pingRouter.Methods("GET", "HEAD").HandlerFunc(pongHandler)

	publicAPIRouter := r.PathPrefix(webPath + "api").Subrouter()
	publicAPIRouter.Use(JSONMiddleware)

	publicAPIRouter.HandleFunc("/auth/login", login).Methods("POST")
	publicAPIRouter.HandleFunc("/auth/logout", logout).Methods("POST")

	authenticatedAPI := r.PathPrefix(webPath + "api").Subrouter()
	authenticatedAPI.Use(JSONMiddleware, authentication)

	authenticatedAPI.Path("/ws").HandlerFunc(sockets.Handler).Methods("GET", "HEAD")
	authenticatedAPI.Path("/info").HandlerFunc(getSystemInfo).Methods("GET", "HEAD")
	authenticatedAPI.Path("/upgrade").HandlerFunc(checkUpgrade).Methods("GET", "HEAD")
	authenticatedAPI.Path("/upgrade").HandlerFunc(doUpgrade).Methods("POST")

	authenticatedAPI.Path("/projects").HandlerFunc(projects.GetProjects).Methods("GET", "HEAD")
	authenticatedAPI.Path("/projects").HandlerFunc(projects.AddProject).Methods("POST")
	authenticatedAPI.Path("/events").HandlerFunc(getAllEvents).Methods("GET", "HEAD")
	authenticatedAPI.HandleFunc("/events/last", getLastEvents).Methods("GET", "HEAD")

	authenticatedAPI.Path("/users").HandlerFunc(getUsers).Methods("GET", "HEAD")
	authenticatedAPI.Path("/users").HandlerFunc(addUser).Methods("POST")

	tokenAPI := authenticatedAPI.PathPrefix("/user").Subrouter()

	tokenAPI.Path("/").HandlerFunc(getUser).Methods("GET", "HEAD")
	tokenAPI.Path("/tokens").HandlerFunc(getAPITokens).Methods("GET", "HEAD")
	tokenAPI.Path("/tokens").HandlerFunc(createAPIToken).Methods("POST")
	tokenAPI.HandleFunc("/tokens/{token_id}", expireAPIToken).Methods("DELETE")

	userAPI := authenticatedAPI.PathPrefix("/users/{user_id}").Subrouter()
	userAPI.Use(getUserMiddleware)

	userAPI.Path("/").HandlerFunc(getUser).Methods("GET", "HEAD")
	userAPI.Path("/").HandlerFunc(updateUser).Methods("PUT")
	userAPI.Path("/").HandlerFunc(deleteUser).Methods("DELETE")
	userAPI.Path("/password").HandlerFunc(updateUserPassword).Methods("POST")

	projectUserAPI := authenticatedAPI.PathPrefix("/project/{project_id}").Subrouter()
	projectUserAPI.Use(projects.ProjectMiddleware)

	projectUserAPI.Path("/").HandlerFunc(projects.GetProject).Methods("GET", "HEAD")
	projectUserAPI.Path("/events").HandlerFunc(getAllEvents).Methods("GET", "HEAD")
	projectUserAPI.HandleFunc("/events/last", getLastEvents).Methods("GET", "HEAD")

	projectUserAPI.Path("/users").HandlerFunc(projects.GetUsers).Methods("GET", "HEAD")

	projectUserAPI.Path("/keys").HandlerFunc(projects.GetKeys).Methods("GET", "HEAD")
	projectUserAPI.Path("/keys").HandlerFunc(projects.AddKey).Methods("POST")

	projectUserAPI.Path("/repositories").HandlerFunc(projects.GetRepositories).Methods("GET", "HEAD")
	projectUserAPI.Path("/repositories").HandlerFunc(projects.AddRepository).Methods("POST")

	projectUserAPI.Path("/inventory").HandlerFunc(projects.GetInventory).Methods("GET", "HEAD")
	projectUserAPI.Path("/inventory").HandlerFunc(projects.AddInventory).Methods("POST")

	projectUserAPI.Path("/environment").HandlerFunc(projects.GetEnvironment).Methods("GET", "HEAD")
	projectUserAPI.Path("/environment").HandlerFunc(projects.AddEnvironment).Methods("POST")

	projectUserAPI.Path("/tasks").HandlerFunc(tasks.GetAllTasks).Methods("GET", "HEAD")
	projectUserAPI.HandleFunc("/tasks/last", tasks.GetLastTasks).Methods("GET", "HEAD")
	projectUserAPI.Path("/tasks").HandlerFunc(tasks.AddTask).Methods("POST")

	projectUserAPI.Path("/templates").HandlerFunc(projects.GetTemplates).Methods("GET", "HEAD")
	projectUserAPI.Path("/templates").HandlerFunc(projects.AddTemplate).Methods("POST")

	projectAdminAPI := authenticatedAPI.PathPrefix("/project/{project_id}").Subrouter()
	projectAdminAPI.Use(projects.ProjectMiddleware, projects.MustBeAdmin)

	projectAdminAPI.Path("/").HandlerFunc(projects.UpdateProject).Methods("PUT")
	projectAdminAPI.Path("/").HandlerFunc(projects.DeleteProject).Methods("DELETE")
	projectAdminAPI.Path("/users").HandlerFunc(projects.AddUser).Methods("POST")

	projectUserManagement := projectAdminAPI.PathPrefix("/users").Subrouter()
	projectUserManagement.Use(projects.UserMiddleware)

	projectUserManagement.HandleFunc("/{user_id}/admin", projects.MakeUserAdmin).Methods("POST")
	projectUserManagement.HandleFunc("/{user_id}/admin", projects.MakeUserAdmin).Methods("DELETE")
	projectUserManagement.HandleFunc("/{user_id}", projects.RemoveUser).Methods("DELETE")

	projectKeyManagement := projectAdminAPI.PathPrefix("/keys").Subrouter()
	projectKeyManagement.Use(projects.KeyMiddleware)

	projectKeyManagement.HandleFunc("/{key_id}", projects.UpdateKey).Methods("PUT")
	projectKeyManagement.HandleFunc("/{key_id}", projects.RemoveKey).Methods("DELETE")

	projectRepoManagement := projectUserAPI.PathPrefix("/repositories").Subrouter()
	projectRepoManagement.Use(projects.RepositoryMiddleware)

	projectRepoManagement.HandleFunc("/{repository_id}", projects.UpdateRepository).Methods("PUT")
	projectRepoManagement.HandleFunc("/{repository_id}", projects.RemoveRepository).Methods("DELETE")

	projectInventoryManagement := projectUserAPI.PathPrefix("/inventory").Subrouter()
	projectInventoryManagement.Use(projects.InventoryMiddleware)

	projectInventoryManagement.HandleFunc("/{inventory_id}", projects.UpdateInventory).Methods("PUT")
	projectInventoryManagement.HandleFunc("/{inventory_id}", projects.RemoveInventory).Methods("DELETE")

	projectEnvManagement := projectUserAPI.PathPrefix("/environment").Subrouter()
	projectEnvManagement.Use(projects.EnvironmentMiddleware)

	projectEnvManagement.HandleFunc("/{environment_id}", projects.UpdateEnvironment).Methods("PUT")
	projectEnvManagement.HandleFunc("/{environment_id}", projects.RemoveEnvironment).Methods("DELETE")

	projectTmplManagement := projectUserAPI.PathPrefix("/templates").Subrouter()
	projectTmplManagement.Use(projects.TemplatesMiddleware)

	projectTmplManagement.HandleFunc("/{template_id}", projects.UpdateTemplate).Methods("PUT")
	projectTmplManagement.HandleFunc("/{template_id}", projects.RemoveTemplate).Methods("DELETE")

	projectTaskManagement := projectUserAPI.PathPrefix("/tasks").Subrouter()
	projectTaskManagement.Use(tasks.GetTaskMiddleware)

	projectTaskManagement.HandleFunc("/{task_id}/output", tasks.GetTaskOutput).Methods("GET", "HEAD")
	projectTaskManagement.HandleFunc("/{task_id}", tasks.GetTask).Methods("GET", "HEAD")
	projectTaskManagement.HandleFunc("/{task_id}", tasks.RemoveTask).Methods("DELETE")

	if os.Getenv("DEBUG") == "1" {
		defer debugPrintRoutes(r)
	}

	return r
}

func debugPrintRoutes(r *mux.Router) {
	err := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			fmt.Println("ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			fmt.Println("Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			fmt.Println("Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			fmt.Println("Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			fmt.Println("Methods:", strings.Join(methods, ","))
		}
		fmt.Println()
		return nil
	})

	if err != nil {
		fmt.Println(err)
	}
}

//nolint: gocyclo
func servePublic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	webPath := "/"
	if util.WebHostURL != nil {
		webPath = util.WebHostURL.RequestURI()
	}

	if !strings.HasPrefix(path, webPath+"public") {
		if len(strings.Split(path, ".")) > 1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		path = "/html/index.html"
	}

	path = strings.Replace(path, webPath+"public/", "", 1)
	split := strings.Split(path, ".")
	suffix := split[len(split)-1]

	res, err := publicAssets.MustBytes(path)
	if err != nil {
		notFoundHandler(w, r)
		return
	}

	// replace base path
	if util.WebHostURL != nil && path == "/html/index.html" {
		res = []byte(strings.Replace(string(res),
			"<base href=\"/\">",
			"<base href=\""+util.WebHostURL.String()+"\">",
			1))
	}

	contentType := "text/plain"
	switch suffix {
	case "png":
		contentType = "image/png"
	case "jpg", "jpeg":
		contentType = "image/jpeg"
	case "gif":
		contentType = "image/gif"
	case "js":
		contentType = "application/javascript"
	case "css":
		contentType = "text/css"
	case "woff":
		contentType = "application/x-font-woff"
	case "ttf":
		contentType = "application/x-font-ttf"
	case "otf":
		contentType = "application/x-font-otf"
	case "html":
		contentType = "text/html"
	}

	w.Header().Set("content-type", contentType)
	_, err = w.Write(res)
	util.LogWarning(err)
}

func getSystemInfo(w http.ResponseWriter, r *http.Request) {
	body := map[string]interface{}{
		"version": util.Version,
		"update":  util.UpdateAvailable,
		"config": map[string]string{
			"dbHost":  util.Config.MySQL.Hostname,
			"dbName":  util.Config.MySQL.DbName,
			"dbUser":  util.Config.MySQL.Username,
			"path":    util.Config.TmpPath,
			"cmdPath": util.FindSemaphore(),
		},
	}

	if util.UpdateAvailable != nil {
		body["updateBody"] = string(blackfriday.MarkdownCommon([]byte(*util.UpdateAvailable.Body)))
	}

	util.WriteJSON(w, http.StatusOK, body)
}

func checkUpgrade(w http.ResponseWriter, r *http.Request) {
	if err := util.CheckUpdate(util.Version); err != nil {
		util.WriteJSON(w, 500, err)
		return
	}

	if util.UpdateAvailable != nil {
		getSystemInfo(w, r)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func doUpgrade(w http.ResponseWriter, r *http.Request) {
	util.LogError(util.DoUpgrade(util.Version))
}
