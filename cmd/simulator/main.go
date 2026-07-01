package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/ljd166/ac-charger-simulator/internal/config"
	"github.com/ljd166/ac-charger-simulator/internal/manager"
	"github.com/ljd166/ac-charger-simulator/internal/telemetry"
	"github.com/ljd166/ac-charger-simulator/internal/webconsole"
)

func main() {
	configPath := flag.String("config", "testdata/config-2chargers.yaml", "path to config YAML")
	web := flag.Bool("web", true, "enable web console")
	flag.Parse()

	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║  AC Charger Simulator v0.2.0 — OCPP 1.6J AC Charger Sim        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("[FATAL] load config: %v", err)
	}
	log.Printf("[INIT] loaded %d chargers from %s", len(cfg.Chargers), *configPath)

	hub := telemetry.NewHub()
	mgr := manager.NewManager(cfg, hub)

	var ws *webconsole.Server
	if *web && cfg.WebConsole.Enabled {
		addr := fmt.Sprintf("%s:%d", cfg.WebConsole.BindAddr, cfg.WebConsole.Port)
		ws = webconsole.NewServer(addr, mgr, hub)
		if err := ws.Start(); err != nil {
			log.Fatalf("[FATAL] web console: %v", err)
		}
		log.Printf("[INIT] Web Console: http://%s", addr)
	}

	// 连接所有桩
	mgr.StartAll()
	log.Printf("[INIT] all chargers connecting...")

	// 信号处理
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("[RUN] press Ctrl+C to stop")
	<-sigCh

	log.Println("[SHUTDOWN] stopping...")
	mgr.StopAll()
	if ws != nil {
		ws.Stop()
	}
	log.Println("[SHUTDOWN] done")
}
