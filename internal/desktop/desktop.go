// Package desktop is the only place in the codebase that touches Wails
// (ADR-0001). It assembles the application, binds the session service to the
// frontend and adapts agent.Emitter onto Wails events.
package desktop

import (
	"fmt"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/happywork/happywork/frontend"
	"github.com/happywork/happywork/internal/agent"
)

// Options configures Run.
type Options struct {
	Manager *agent.Manager
	// Emitter is the late-binding event bridge created by NewEmitter before
	// Run; it starts forwarding as soon as the app exists.
	Emitter agent.Emitter
	Logger  *log.Logger
}

// lateEmitter forwards events to the Wails app once Run has created it;
// earlier events are dropped (the frontend is not listening yet anyway).
type lateEmitter struct {
	mu  sync.Mutex
	app *application.App
}

func (e *lateEmitter) bind(app *application.App) {
	e.mu.Lock()
	e.app = app
	e.mu.Unlock()
}

func (e *lateEmitter) Emit(name string, data any) {
	e.mu.Lock()
	app := e.app
	e.mu.Unlock()
	if app != nil {
		app.Event.Emit(name, data)
	}
}

// NewEmitter creates the event bridge to hand to agent.NewManager before
// desktop.Run binds it to the real application.
func NewEmitter() agent.Emitter { return &lateEmitter{} }

// currentMu guards window access for focus requests from the single-instance
// watcher goroutine.
var (
	currentMu sync.Mutex
	window    *application.WebviewWindow
)

// FocusMainWindow focuses the main window; safe to call before Run has
// created any window (no-op then).
func FocusMainWindow() {
	currentMu.Lock()
	defer currentMu.Unlock()
	if window != nil {
		window.Focus()
	}
}

// Run builds the Wails application and blocks until it exits.
func Run(opts Options) error {
	if opts.Manager == nil {
		return fmt.Errorf("desktop: nil manager")
	}
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	bridge, ok := opts.Emitter.(*lateEmitter)
	if !ok {
		return fmt.Errorf("desktop: Run requires Options.Emitter from NewEmitter (got %T); "+
			"without it frontend events are silently dropped", opts.Emitter)
	}

	app := application.New(application.Options{
		Name:        "HappyWork",
		Description: "本地优先、权限受控的桌面 AI Agent",
		Services: []application.Service{
			application.NewService(&SessionService{mgr: opts.Manager}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontend.Dist()),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "HappyWork",
		Width:  1100,
		Height: 720,
		URL:    "/",
	})

	bridge.bind(app)
	currentMu.Lock()
	window = win
	currentMu.Unlock()

	opts.Logger.Printf("desktop: running")
	return app.Run()
}
