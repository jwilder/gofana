package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/go-martini/martini"
	"github.com/jwilder/gofana/grafana"
	"github.com/martini-contrib/auth"
	"github.com/martini-contrib/render"
	"github.com/martini-contrib/secure"
	"github.com/martini-contrib/sessions"
	"github.com/martini-contrib/staticbin"
	"github.com/stretchr/gomniauth"
	"github.com/stretchr/gomniauth/providers/google"
	"github.com/stretchr/objx"
	"github.com/stretchr/signature"

	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

const (
	defaultSessionSecret = "e40f5e3a62f25ef48eeb03440735831a"
)

var (
	db                                 *DashboardRepository
	wg                                 sync.WaitGroup
	basicAuth, authDomain              string
	httpAddr, httpsAddr                string
	sslCert, sslKey                    string
	appDir, dbDir                      string
	graphiteURL                        string
	influxDBURL                        string
	influxDBUser                       string
	influxDBPass                       string
	openTSDBUrl                        string
	buildVersion                       string
	hostAddr                           string
	googleClientID, googleClientSecret string
	sessionSecret                      string
	version                            bool
)

func addCorsHeaders(w http.ResponseWriter) {
	w.Header().Add("Access-Control-Allow-Headers", "X-Requested-With, Content-Type, Content-Length")
	w.Header().Add("Access-Control-Allow-Methods", "OPTIONS, HEAD, GET, POST, PUT, DELETE")
	w.Header().Add("Access-Control-Allow-Origin", "*")
}

func loginRequired(s sessions.Session, c martini.Context, w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/oauth2callback") ||
		strings.HasSuffix(r.URL.Path, "/signin") ||
		strings.HasSuffix(r.URL.Path, "/auth") {
		return
	}

	if s.Get("username") == nil {
		http.Redirect(w, r, "/signin", http.StatusFound)
	}
}

func authRedirect(params martini.Params, s sessions.Session, c martini.Context, w http.ResponseWriter, r *http.Request) {
	providerName := params["provider"]
	if providerName == "" {
		http.Error(w, "Unknown provider", http.StatusBadRequest)
		return
	}

	provider, err := gomniauth.Provider(providerName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	authUrl, err := provider.GetBeginAuthURL(nil, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}
	http.Redirect(w, r, authUrl, http.StatusFound)
}

func getSignin(s sessions.Session, w http.ResponseWriter, r *http.Request) {
	s.Delete("username")
	if !oauthEnabled() {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	body, err := Asset("templates/signin.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	tmpl, err := template.New("config.js").Parse(string(body))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	err = tmpl.Execute(w, struct {
		Error  string
		Google bool
	}{
		Error:  r.FormValue("error"),
		Google: googleOauthEnabled(),
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}
}

func oauth2callback(params martini.Params, s sessions.Session, c martini.Context, w http.ResponseWriter, r *http.Request) {
	providerName := params["provider"]
	if providerName == "" {
		http.Error(w, "Unknown provider", http.StatusBadRequest)
		return
	}

	provider, err := gomniauth.Provider(providerName)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	omap, err := objx.FromURLQuery(r.URL.RawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	creds, err := provider.CompleteAuth(omap)
	if err != nil {
		s.Delete("username")
		http.Redirect(w, r, "/signin?error=Access+Denied", http.StatusFound)
		log.Printf("ERROR: %s", err)
		return
	}

	user, err := provider.GetUser(creds)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	if strings.HasSuffix(user.Email(), authDomain) {
		log.Printf("%s authenticated via %s", user.Email(), providerName)
		s.Set("username", user.Email())
	} else {
		http.Redirect(w, r, "/signin", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

func saveDashboard(w http.ResponseWriter, r *http.Request, params martini.Params) {
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	err = db.Save(params["id"], body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	w.Write([]byte("{}"))
}

func getDashboard(w http.ResponseWriter, r *http.Request, params martini.Params) {
	id := params["id"]
	if !db.Exists(id) {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	data, err := db.Get(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	w.Header().Add("Content-Length", strconv.FormatInt(int64(len(string(data))), 10))
	w.Write(data)
}

func deleteDashboard(w http.ResponseWriter, r *http.Request, params martini.Params) {
	id := params["id"]
	err := db.Delete(id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}
}

func searchDashboards(w http.ResponseWriter, r *http.Request) {
	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Printf("ERROR: %s", err)
		return
	}

	dashboards, err := db.Search(r.Form.Get("query"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		log.Printf("ERROR: %s", err)
		return
	}

	w.Header().Add("Content-Type", "application/json")

	body, err := json.Marshal(struct {
		Dashboards []*Dashboard `json:"dashboards"`
	}{Dashboards: dashboards})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return

	}
	w.Write(body)
}

func gofanaDatasource(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "application/json")
	body, err := Asset("templates/datasource.gofana.js")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	w.Write(body)
}

func gofanaConfig(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "application/json")
	body, err := Asset("templates/config.js")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	tmpl, err := template.New("config.js").Parse(string(body))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	err = tmpl.Execute(w, struct {
		GraphiteURL  string
		InfluxDBURL  string
		InfluxDBUser string
		InfluxDBPass string
		OpenTSDBUrl  string
	}{
		GraphiteURL:  graphiteURL,
		InfluxDBURL:  influxDBURL,
		InfluxDBUser: influxDBUser,
		InfluxDBPass: influxDBPass,
		OpenTSDBUrl:  openTSDBUrl,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}
}

func copyHeader(source http.Header, dest *http.Header) {
	for n, v := range source {
		for _, vv := range v {
			dest.Add(n, vv)
		}
	}
}

func proxyOpenTSDB(w http.ResponseWriter, r *http.Request) {
	proxy(openTSDBUrl, w, r)
}

func proxyGraphite(w http.ResponseWriter, r *http.Request) {
	proxy(graphiteURL, w, r)
}
func proxyInfluxDB(w http.ResponseWriter, r *http.Request) {
	proxy(influxDBURL, w, r)
}
func proxy(target string, w http.ResponseWriter, r *http.Request) {

	stripped := r.RequestURI[strings.Index(r.RequestURI[1:], "/")+1:]
	uri := target + stripped

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	rr, err := http.NewRequest(r.Method, uri, bytes.NewBuffer(body))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}
	copyHeader(r.Header, &rr.Header)

	// Create a client and query the target
	var transport http.Transport
	resp, err := transport.RoundTrip(rr)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("ERROR: %s", err)
		return
	}

	dH := w.Header()
	copyHeader(resp.Header, &dH)

	w.Write(body)
}

func googleOauthEnabled() bool {
	return googleClientID != "" && googleClientSecret != ""
}
func oauthEnabled() bool {
	return googleOauthEnabled()
}

func main() {

	flag.StringVar(&appDir, "app-dir", "", "Path to grafana installation")
	flag.StringVar(&dbDir, "db-dir", "dashboards", "Path to dashboard storage dir")
	flag.StringVar(&authDomain, "auth-domain", "", "OAuth2 domain users must authenticated from (mydomain.com)")
	flag.StringVar(&basicAuth, "auth", "", "Basic auth username (user:pw)")
	flag.StringVar(&sessionSecret, "session-secret", defaultSessionSecret, "Session secret key")
	flag.StringVar(&httpAddr, "http-addr", ":8080", "HTTP Server bind address")
	flag.StringVar(&httpsAddr, "https-addr", ":8443", "HTTPS Server bind address")
	flag.StringVar(&graphiteURL, "graphite-url", "", "Graphite URL (http://host:port)")
	flag.StringVar(&influxDBURL, "influxdb-url", "", "InfluxDB URL (http://host:8086/db/mydb)")
	flag.StringVar(&influxDBUser, "influxdb-user", "", "InfluxDB username")
	flag.StringVar(&influxDBPass, "influxdb-pass", "", "InfluxDB password")
	flag.StringVar(&openTSDBUrl, "opentsdb-url", "", "OpenTSDB URL (http://host:4242)")
	flag.StringVar(&sslCert, "ssl-cert", "", "SSL cert (PEM formatted)")
	flag.StringVar(&sslKey, "ssl-key", "", "SSL key (PEM formatted)")
	flag.StringVar(&hostAddr, "host-addr", "http://localhost:8080", "Public server address (http://mydomain.com)")
	flag.StringVar(&googleClientID, "google-client-id", "", "Google Oauth2 Client ID")
	flag.StringVar(&googleClientSecret, "google-client-secret", "", "Google Oauth2 Client Sercret")

	flag.BoolVar(&version, "version", false, "show version")
	flag.Parse()

	if version {
		println(buildVersion)
		return
	}

	if sessionSecret == defaultSessionSecret {
		log.Printf("WARN: Session secret key is using the hard-coded default. Use -session-secret <value> for a live deployment.\n")
	}

	if graphiteURL == "" && influxDBURL == "" && openTSDBUrl == "" {
		fmt.Printf("No graphite-url, influxdb-url or opentsdb-url specified.\nUse -graphite-url http://host:port or -influxdb-url http://host:8086/db/mydb or -opentsdb-url http://host:4242\n")
		return
	}

	log.Printf("Starting gofana %s", buildVersion)
	if _, err := os.Stat(dbDir); os.IsNotExist(err) {
		fmt.Printf("%s does not exist. Creating.\n", dbDir)
		err := os.Mkdir(dbDir, 0766)
		if err != nil {
			fmt.Printf("ERROR: %s\n", err)
			return
		}
	}

	db = &DashboardRepository{Dir: dbDir}
	err := db.Load()
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}

	logger := log.New(os.Stderr, "", log.LstdFlags)
	r := martini.NewRouter()
	m := martini.New()
	m.Map(logger)
	m.Use(martini.Recovery())
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)

	if sslCert != "" && sslKey != "" {
		m.Use(secure.Secure(secure.Options{}))
	}

	b := make([]byte, 32)
	_, err = rand.Read(b)
	if err != nil {
		fmt.Printf("ERROR: %s\n", err)
		return
	}

	m.Use(sessions.Sessions("session", sessions.NewCookieStore([]byte(sessionSecret))))
	if oauthEnabled() {

		if authDomain == "" {
			fmt.Println("ERROR: No -auth-domain specified.  Cannot authenticate with OAuth2.\n")
			return
		}

		gomniauth.SetSecurityKey(signature.RandomKey(64))
		providers := gomniauth.WithProviders()

		if googleOauthEnabled() {
			providers.Add(google.New(googleClientID, googleClientSecret, fmt.Sprintf("%s/google/oauth2callback", hostAddr)))
		}
		m.Use(loginRequired)
	}

	m.Use(addCorsHeaders)
	m.Use(render.Renderer())

	if basicAuth != "" && strings.Contains(basicAuth, ":") {
		parts := strings.Split(basicAuth, ":")
		m.Use(auth.Basic(parts[0], parts[1]))
	}

	var static martini.Handler
	if appDir == "" {
		static = staticbin.Static("grafana-1.9.1", grafana.Asset)
	} else {
		static = martini.Static(appDir, martini.StaticOptions{Fallback: "/index.html", Exclude: "/api/v"})
	}

	r.NotFound(static, http.NotFound)

	r.Get("/search", searchDashboards)
	r.Get("/dashboard/:id", getDashboard)
	r.Post("/dashboard/:id", saveDashboard)
	r.Delete("/dashboard/:id", deleteDashboard)
	r.Get("/plugins/datasource.gofana.js", gofanaDatasource)
	r.Get("/config.js", gofanaConfig)
	r.Get("/graphite/**", proxyGraphite)
	r.Post("/graphite/**", proxyGraphite)
	r.Get("/influxdb/**", proxyInfluxDB)
	r.Post("/influxdb/**", proxyInfluxDB)
	r.Get("/opentsdb/**", proxyOpenTSDB)
	r.Post("/opentsdb/**", proxyOpenTSDB)
	r.Get("/:provider/auth", authRedirect)
	r.Get("/:provider/oauth2callback", oauth2callback)
	r.Get("/signin", getSignin)

	// HTTP Listener
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Printf("HTTP listening on %s\n", httpAddr)
		if err := http.ListenAndServe(httpAddr, m); err != nil {
			log.Fatal(err)
		}
	}()

	// HTTPS Listener
	if sslCert != "" && sslKey != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Printf("HTTPS listening on %s", httpsAddr)
			if err := http.ListenAndServeTLS(httpsAddr, sslCert, sslKey, m); err != nil {
				log.Fatal(err)

			}
		}()
	}
	wg.Wait()
}
