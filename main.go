package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	_ "embed"

	"fyne.io/systray"
)

//go:embed assets/tailscale-icon.png
var iconData []byte

func main() {
	// Run the tray. systray.Run blocks until systray.Quit() is called
	// onReady runs after the tray is initialized. onExit runs on shutdown
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconData)
	systray.SetTitle("Tailstrayle")
	systray.SetTooltip("Tailscale Tray")

	mLogin := systray.AddMenuItem("Login", "Log into Tailsale account")

	mConnect := systray.AddMenuItem("Connect", "Connect to Tailnet")

	mQuit := systray.AddMenuItem("Quit", "Exit the app")

	// Handle Ctrl+C / SIGTERM so tray exits cleanly
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-mLogin.ClickedCh:
			log.Println("login clicked")
		case <-mConnect.ClickedCh:
			log.Println("connect clicked")
		case <-mQuit.ClickedCh:
			log.Println("quit clicked")
		case sig := <-sigCh:
			log.Printf("received signal: %v", sig)
		}
		systray.Quit()
	}()
}

func onExit() {
	log.Println("shutting down")
}
