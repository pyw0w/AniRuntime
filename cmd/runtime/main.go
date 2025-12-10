package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pyw0w/AniApi/shared"
	"github.com/pyw0w/AniCore/pkg/config"
	coreloader "github.com/pyw0w/AniCore/pkg/coreloader"
	"github.com/pyw0w/AniCore/pkg/database"
	"github.com/pyw0w/AniCore/pkg/eventbus"
	"github.com/pyw0w/AniCore/pkg/logger"
	pluginloader "github.com/pyw0w/AniCore/pkg/pluginloader"
	"github.com/pyw0w/AniCore/pkg/security"
)

// Runtime представляет основной runtime
type Runtime struct {
	config       *config.Config
	eventBus     *eventbus.EventBus
	pluginLoader *pluginloader.Loader
	coreLoader   *coreloader.Loader
	verifier     *security.Verifier
	plugins      map[string]shared.Plugin
	cores        map[string]shared.Core
	coreAPIs     map[string]*CoreAPIImpl
	log          *logger.Logger
	coreVersion  string
	database     *database.DB
}

// CoreAPIImpl реализует shared.CoreAPI для плагинов
type CoreAPIImpl struct {
	runtime *Runtime
	plugin  shared.Plugin
}

func (api *CoreAPIImpl) Log(format string, args ...interface{}) {
	api.runtime.log.WithPrefix(fmt.Sprintf("Plugin:%s", api.plugin.Name())).Info(format, args...)
}

func (api *CoreAPIImpl) Emit(e shared.Event) error {
	return api.runtime.eventBus.Publish(e)
}

func (api *CoreAPIImpl) CallCore(coreName string, cmd shared.Event) error {
	core, exists := api.runtime.cores[coreName]
	if !exists {
		return fmt.Errorf("core %s not found", coreName)
	}
	return core.OnCommand(cmd)
}

func (api *CoreAPIImpl) Database() shared.DatabaseAPI {
	return api.runtime.GetDatabaseAPI(api.plugin.Name())
}

// RuntimeAPIImpl реализует shared.RuntimeAPI для core-модулей
type RuntimeAPIImpl struct {
	runtime *Runtime
	core    shared.Core
	config  map[string]interface{}
}

func (api *RuntimeAPIImpl) Log(format string, args ...interface{}) {
	api.runtime.log.WithPrefix(fmt.Sprintf("Core:%s", api.core.Name())).Info(format, args...)
}

func (api *RuntimeAPIImpl) Emit(e shared.Event) error {
	return api.runtime.eventBus.Publish(e)
}

func (api *RuntimeAPIImpl) RegisterCore(core shared.Core) error {
	// Core уже зарегистрирован при загрузке
	return nil
}

func (api *RuntimeAPIImpl) GetConfig() map[string]interface{} {
	return api.config
}

func (api *RuntimeAPIImpl) GetPluginCommands() []*discordgo.ApplicationCommand {
	return api.runtime.GetPluginCommands()
}

func (api *RuntimeAPIImpl) GetCoreVersion() string {
	return api.runtime.GetCoreVersion()
}

func (api *RuntimeAPIImpl) GetDatabase() *sql.DB {
	if api.runtime.database == nil {
		return nil
	}
	return api.runtime.database.DB
}

// NewRuntime создает новый Runtime
func NewRuntime(cfg *config.Config) *Runtime {
	bus := eventbus.NewEventBus()
	verifier := security.NewVerifier(cfg.Security.AllowedPaths, cfg.Security.VerifyHashes)

	// Загружаем версию ядра из файла VERSION
	coreVersion := "1.0.0" // значение по умолчанию
	if versionData, err := os.ReadFile("VERSION"); err == nil {
		coreVersion = strings.TrimSpace(string(versionData))
	} else {
		// Пробуем загрузить из родительской директории (AniCore)
		if versionData, err := os.ReadFile("../VERSION"); err == nil {
			coreVersion = strings.TrimSpace(string(versionData))
		}
	}

	// Создаем логгер с уровнем из конфига
	logLevel := logger.ParseLevel(cfg.LogLevel)
	runtimeLog := logger.New("Runtime", logLevel)

	// Инициализация БД (если backend включен)
	var db *database.DB
	if cfg.Backend != nil && cfg.Backend.Enabled {
		dbPath := cfg.Backend.Database.Path
		if dbPath == "" {
			dbPath = "./data/anicore.db"
		}
		var err error
		db, err = database.NewDB(dbPath)
		if err != nil {
			runtimeLog.Warn("Failed to initialize database: %v", err)
		} else {
			runtimeLog.Info("Database initialized: %s", dbPath)
		}
	}

	return &Runtime{
		config:       cfg,
		eventBus:     bus,
		pluginLoader: pluginloader.NewLoader(),
		coreLoader:   coreloader.NewLoader(),
		verifier:     verifier,
		plugins:      make(map[string]shared.Plugin),
		cores:        make(map[string]shared.Core),
		coreAPIs:     make(map[string]*CoreAPIImpl),
		log:          runtimeLog,
		coreVersion:  coreVersion,
		database:     db,
	}
}

// LoadCores загружает core-модули
func (r *Runtime) LoadCores() error {
	r.log.Info("Loading core modules...")

	for _, coreName := range r.config.Cores {
		// Используем CoreDirectory, если указан, иначе CoresDirectory (обратная совместимость)
		coreDir := r.config.CoreDirectory
		if coreDir == "" {
			coreDir = r.config.CoresDirectory
		}
		corePath := filepath.Join(coreDir, coreName+".so")

		r.log.Info("Loading core: %s from %s", coreName, corePath)

		core, err := r.coreLoader.LoadCore(corePath)
		if err != nil {
			return fmt.Errorf("failed to load core %s: %w", coreName, err)
		}

		// Получаем конфигурацию для core
		var coreConfig map[string]interface{}
		if r.config.CoreConfigs != nil {
			if cfg, exists := r.config.CoreConfigs[coreName]; exists {
				coreConfig = cfg.Config
			}
		}

		// Инициализируем core
		runtimeAPI := &RuntimeAPIImpl{
			runtime: r,
			core:    core,
			config:  coreConfig,
		}

		if err := core.Init(runtimeAPI); err != nil {
			return fmt.Errorf("failed to init core %s: %w", coreName, err)
		}

		r.cores[coreName] = core
		r.log.Info("Core %s loaded successfully", coreName)
	}

	return nil
}

// StartCores запускает все загруженные core-модули
func (r *Runtime) StartCores() error {
	r.log.Info("Starting core modules...")

	for name, core := range r.cores {
		if err := core.Start(); err != nil {
			return fmt.Errorf("failed to start core %s: %w", name, err)
		}
		r.log.Info("Core %s started", name)
	}

	return nil
}

// StopCores останавливает все core-модули
func (r *Runtime) StopCores() error {
	r.log.Info("Stopping core modules...")

	for name, core := range r.cores {
		if err := core.Stop(); err != nil {
			r.log.Warn("Error stopping core %s: %v", name, err)
		} else {
			r.log.Debug("Core %s stopped", name)
		}
	}

	return nil
}

func (r *Runtime) upsertPluginRecord(id, name, version string, enabled bool) {
	if r.database == nil || r.database.DB == nil {
		return
	}

	query := `
		INSERT INTO plugins (id, name, version, enabled, loaded_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			version = excluded.version,
			enabled = excluded.enabled,
			updated_at = CURRENT_TIMESTAMP
	`

	if _, err := r.database.DB.Exec(query, id, name, version, enabled); err != nil {
		r.log.Warn("Failed to upsert plugin record %s: %v", id, err)
	}
}

// LoadPlugins загружает плагины
func (r *Runtime) LoadPlugins() error {
	r.log.Info("Loading plugins...")

	pluginDir := r.config.Plugins.Directory
	enabledPlugins := make(map[string]bool)
	for _, name := range r.config.Plugins.Enabled {
		enabledPlugins[name] = true
	}

	// Сканируем директорию плагинов
	entries, err := os.ReadDir(pluginDir)
	if err != nil {
		return fmt.Errorf("failed to read plugins directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if filepath.Ext(entry.Name()) != ".so" {
			continue
		}

		pluginName := entry.Name()[:len(entry.Name())-3] // убираем .so

		// Проверяем, включен ли плагин
		if !enabledPlugins[pluginName] {
			r.log.Debug("Plugin %s is not enabled, skipping", pluginName)
			continue
		}

		pluginPath := filepath.Join(pluginDir, entry.Name())

		// Проверяем безопасность
		if err := r.verifier.Verify(pluginPath); err != nil {
			r.log.Warn("Security check failed for plugin %s: %v", pluginName, err)
			continue
		}

		r.log.Info("Loading plugin: %s from %s", pluginName, pluginPath)

		plugin, err := r.pluginLoader.LoadPlugin(pluginPath, r.coreVersion)
		if err != nil {
			r.log.Error("Failed to load plugin %s: %v", pluginName, err)
			continue
		}

		// Создаем CoreAPI для плагина
		coreAPI := &CoreAPIImpl{
			runtime: r,
			plugin:  plugin,
		}

		// Регистрируем или обновляем запись о плагине в БД (до Init, чтобы plugin_data имел валидный FK)
		r.upsertPluginRecord(plugin.Name(), plugin.Name(), plugin.Version(), true)

		// Инициализируем плагин
		if err := plugin.Init(coreAPI); err != nil {
			r.log.Error("Failed to init plugin %s: %v", pluginName, err)
			continue
		}

		r.plugins[pluginName] = plugin
		r.coreAPIs[pluginName] = coreAPI

		// Подписываем плагин на события через event bus
		// Все плагины подписываются на message_create и command
		r.eventBus.Subscribe(shared.EventTypeMessageCreate, plugin)
		r.eventBus.Subscribe(shared.EventTypeCommand, plugin)

		r.log.Info("Plugin %s (v%s) loaded successfully", plugin.Name(), plugin.Version())

		// Отправляем событие о загрузке плагина
		event := shared.NewEvent(string(shared.EventTypePluginLoaded), map[string]string{
			"plugin": pluginName,
		}, "runtime")
		r.eventBus.Publish(event)
	}

	return nil
}

// GetPluginCommands собирает команды от всех плагинов, реализующих CommandProvider
func (r *Runtime) GetPluginCommands() []*discordgo.ApplicationCommand {
	var allCommands []*discordgo.ApplicationCommand

	for pluginName, plugin := range r.plugins {
		// Проверяем, реализует ли плагин интерфейс CommandProvider
		if commandProvider, ok := plugin.(shared.CommandProvider); ok {
			commands := commandProvider.GetCommands()
			r.log.Debug("Plugin %s provided %d commands", pluginName, len(commands))
			allCommands = append(allCommands, commands...)
		}
	}

	return allCommands
}

// GetCoreVersion возвращает версию ядра
func (r *Runtime) GetCoreVersion() string {
	return r.coreVersion
}

// GetDatabaseAPI возвращает DatabaseAPI для плагина
func (r *Runtime) GetDatabaseAPI(pluginID string) shared.DatabaseAPI {
	if r.database == nil {
		// Возвращаем nil, если БД не инициализирована
		return nil
	}
	return database.NewDatabaseAPI(r.database.DB, pluginID)
}

// Run запускает runtime
func (r *Runtime) Run(ctx context.Context) error {
	// Загружаем core-модули
	if err := r.LoadCores(); err != nil {
		return fmt.Errorf("failed to load cores: %w", err)
	}

	// Запускаем core-модули
	if err := r.StartCores(); err != nil {
		return fmt.Errorf("failed to start cores: %w", err)
	}

	// Загружаем плагины
	if err := r.LoadPlugins(); err != nil {
		return fmt.Errorf("failed to load plugins: %w", err)
	}

	r.log.Info("Runtime started successfully")

	// Ожидаем сигнала завершения
	<-ctx.Done()

	// Останавливаем core-модули
	if err := r.StopCores(); err != nil {
		r.log.Warn("Error stopping cores: %v", err)
	}

	r.log.Info("Runtime stopped")
	return nil
}

func main() {
	configPath := flag.String("config", "config.json", "Path to configuration file")
	flag.Parse()

	// Загружаем конфигурацию
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Создаем runtime
	runtime := NewRuntime(cfg)

	// Настраиваем обработку сигналов
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		fmt.Printf("Received signal: %v, shutting down...\n", sig)
		cancel()
	}()

	// Запускаем runtime
	if err := runtime.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Runtime error: %v\n", err)
		os.Exit(1)
	}

	// Даем время на graceful shutdown
	time.Sleep(1 * time.Second)
}
