package session_test

import (
	"fmt"
	"net/http"
	"time"

	"github.com/kusold/gotchi/session"
)

// ExampleNewMemory demonstrates creating an in-memory session manager with
// custom configuration and using it as HTTP middleware.
func ExampleNewMemory() {
	mgr := session.NewMemory(session.Config{
		Lifetime:       12 * time.Hour,
		CookieName:     "myapp-session",
		CookieSecure:   true,
		CookieSameSite: http.SameSiteStrictMode,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Store a value in the session.
		mgr.Put(r.Context(), "username", "alice")

		// Retrieve it in the same or a subsequent request.
		username := mgr.GetString(r.Context(), "username")
		fmt.Fprintf(w, "Hello, %s!", username)
	})

	// Wrap the handler with session middleware.
	http.Handle("/", mgr.LoadAndSave(handler))
}

// ExampleNewMemory_defaultConfig demonstrates creating an in-memory session
// manager using all default configuration values.
func ExampleNewMemory_defaultConfig() {
	mgr := session.NewMemory(session.Config{})

	// Defaults applied:
	//   ExpiryInterval = 5 minutes
	//   Lifetime       = 24 hours
	//   CookieName     = "session"
	//   CookieSecure   = false
	//   CookieSameSite = http.SameSiteLaxMode

	inner := mgr.Inner()
	fmt.Println("Cookie name:", inner.Cookie.Name)
	fmt.Println("Lifetime:", inner.Lifetime)
	// Output:
	// Cookie name: session
	// Lifetime: 24h0m0s
}

// ExampleNewPostgres demonstrates creating a PostgreSQL-backed session manager.
func ExampleNewPostgres() {
	// In a real application, obtain the pool from your database configuration.
	// pool, err := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer pool.Close()

	// For this example, pool is nil; replace with a real pool in production.
	var pool interface{} // placeholder — use *pgxpool.Pool in real code
	_ = pool

	// Uncomment the following lines when you have a real pool:
	// mgr := session.NewPostgres(session.Config{
	//     Lifetime:   24 * time.Hour,
	//     CookieName: "prod-session",
	// }, pool, "user_sessions")

	fmt.Println("Use NewPostgres with a real *pgxpool.Pool in production")
	// Output:
	// Use NewPostgres with a real *pgxpool.Pool in production
}

// ExampleRegisterGobTypes demonstrates registering custom types so they can be
// stored in sessions. Custom types must be registered before any session
// read or write operations.
func ExampleRegisterGobTypes() {
	// Define a custom type you want to store in sessions.
	type UserProfile struct {
		ID   int
		Name string
		Role string
	}

	// Register the type with gob before using it in sessions.
	session.RegisterGobTypes(UserProfile{})

	// Now UserProfile values can safely be stored and retrieved:
	// mgr.Put(r.Context(), "profile", UserProfile{ID: 1, Name: "Alice", Role: "admin"})
}

// ExampleManager_LoadAndSave demonstrates using LoadAndSave as middleware to
// automatically manage session cookies across HTTP requests.
func ExampleManager_LoadAndSave() {
	mgr := session.NewMemory(session.Config{
		Lifetime:   1 * time.Hour,
		CookieName: "example-session",
	})

	// Use LoadAndSave as middleware in your router or handler chain.
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Count page views in the session.
		views, _ := mgr.Get(r.Context(), "page_views").(int)
		views++
		mgr.Put(r.Context(), "page_views", views)
		fmt.Fprintf(w, "Page views: %d", views)
	})

	// Wrap the entire mux with session middleware.
	http.ListenAndServe(":8080", mgr.LoadAndSave(mux))
}

// ExampleManager_Put demonstrates storing values of various types in the
// session using the context obtained from an HTTP request.
func ExampleManager_Put() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr // In a real handler, use mgr.LoadAndSave as middleware first.

	// Inside an HTTP handler wrapped by LoadAndSave:
	// mgr.Put(r.Context(), "user_id", 42)
	// mgr.Put(r.Context(), "is_admin", true)
	// mgr.Put(r.Context(), "preferences", map[string]string{"theme": "dark"})
}

// ExampleManager_Get demonstrates retrieving a value from the session with a
// type assertion.
func ExampleManager_Get() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr // In a real handler, use mgr.LoadAndSave as middleware first.

	// Inside an HTTP handler wrapped by LoadAndSave:
	// val := mgr.Get(r.Context(), "user_id")
	// if id, ok := val.(int); ok {
	//     fmt.Fprintf(w, "User ID: %d", id)
	// }
}

// ExampleManager_GetString demonstrates retrieving a string value from the
// session.
func ExampleManager_GetString() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr // In a real handler, use mgr.LoadAndSave as middleware first.

	// Inside an HTTP handler wrapped by LoadAndSave:
	// mgr.Put(r.Context(), "username", "bob")
	// username := mgr.GetString(r.Context(), "username")
	// fmt.Fprintf(w, "Welcome, %s!", username)
}

// ExampleManager_PutHTTP demonstrates the convenience method for storing a
// session value directly from an HTTP request, without manually extracting the
// context.
func ExampleManager_PutHTTP() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr // In a real handler, use mgr.LoadAndSave as middleware first.

	// Inside an HTTP handler wrapped by LoadAndSave:
	// mgr.PutHTTP(r, "flash_message", "Record saved successfully")
	//
	// This is equivalent to:
	// mgr.Put(r.Context(), "flash_message", "Record saved successfully")
}

// ExampleManager_Load demonstrates loading a session from a token outside the
// standard middleware flow, such as in a WebSocket upgrade handler.
func ExampleManager_Load() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr

	// When you have a session token (e.g., from a query parameter or custom
	// header) and need to load the session outside the standard middleware:
	// ctx := context.Background()
	// sessionCtx, err := mgr.Load(ctx, token)
	// if err != nil {
	//     log.Printf("failed to load session: %v", err)
	//     return
	// }
	// userID := mgr.Get(sessionCtx, "user_id")
}

// ExampleManager_Destroy demonstrates destroying a session, typically used for
// logout functionality.
func ExampleManager_Destroy() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr

	// Inside a logout handler wrapped by LoadAndSave:
	// if err := mgr.Destroy(r.Context()); err != nil {
	//     http.Error(w, "failed to destroy session", http.StatusInternalServerError)
	//     return
	// }
	// http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ExampleManager_Inner demonstrates accessing the underlying SCS SessionManager
// for advanced configuration or features not exposed by the Manager wrapper.
func ExampleManager_Inner() {
	mgr := session.NewMemory(session.Config{
		CookieName: "advanced-session",
	})

	inner := mgr.Inner()
	_ = inner

	// Access advanced SCS features:
	// inner.Cookie.HttpOnly = true
	// inner.Cookie.Domain = "example.com"
	// inner.IdleTimeout = 30 * time.Minute
	// token := inner.Token(r.Context())
}

// ExampleNew demonstrates creating a Manager with a custom store
// implementation. This is useful when you need a store backend not covered
// by [session.NewMemory] or [session.NewPostgres].
func ExampleNew() {
	// Create a Manager with a custom store (e.g., a Redis store).
	// store := redisstore.New(redisClient)
	// mgr := session.New(session.Config{
	//     Lifetime:       6 * time.Hour,
	//     CookieName:     "custom-store-session",
	//     CookieSecure:   true,
	//     CookieSameSite: http.SameSiteStrictMode,
	// }, store)

	// For demonstration, show how defaults are applied:
	mgr := session.NewMemory(session.Config{})
	inner := mgr.Inner()
	fmt.Println(inner.Cookie.Name)
	// Output: session
}

// ExampleConfig_withDefaults demonstrates how Config zero-values are
// populated with sensible defaults.
func ExampleConfig() {
	// All zero-value fields receive defaults:
	cfg := session.Config{}

	// The constructors apply defaults internally, but you can inspect the
	// resulting configuration via the underlying manager.
	mgr := session.NewMemory(cfg)
	inner := mgr.Inner()

	fmt.Println("Lifetime:", inner.Lifetime)
	fmt.Println("SameSite:", inner.Cookie.SameSite)
	// Output:
	// Lifetime: 24h0m0s
	// SameSite: 2
}

// Example_contextFlow demonstrates the full lifecycle of session data across
// multiple HTTP requests using a shared Manager.
func Example_contextFlow() {
	mgr := session.NewMemory(session.Config{
		Lifetime:   30 * time.Minute,
		CookieName: "lifecycle-demo",
	})
	_ = mgr

	// Request 1 — store data:
	// handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	//     mgr.Put(r.Context(), "cart_items", 3)
	//     w.WriteHeader(http.StatusOK)
	// })

	// Request 2 — retrieve data (using the session cookie from request 1):
	// retrieveHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	//     items := mgr.Get(r.Context(), "cart_items")
	//     fmt.Fprintf(w, "Cart items: %v", items)
	// })

	// Request 3 — destroy session (logout):
	// logoutHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	//     mgr.Destroy(r.Context())
	//     http.Redirect(w, r, "/", http.StatusSeeOther)
	// })
}

// Example_backgroundWorker demonstrates loading a session from a token in a
// non-HTTP context, such as a background job or WebSocket handler.
func Example_backgroundWorker() {
	mgr := session.NewMemory(session.Config{})
	_ = mgr

	// In a background worker or WebSocket handler, load a session from a token:
	// ctx := context.Background()
	// sessionCtx, err := mgr.Load(ctx, sessionToken)
	// if err != nil {
	//     log.Fatal(err)
	// }
	// data := mgr.Get(sessionCtx, "background_job_state")
}
