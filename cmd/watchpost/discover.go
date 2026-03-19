package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/rvben/watchpost/internal/camera"
)

func runDiscover() {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	timeout := fs.Duration("timeout", 5*time.Second, "discovery timeout duration")
	yamlOutput := fs.Bool("yaml", false, "output config YAML snippet")
	probeRTSP := fs.Bool("probe-rtsp", false, "probe discovered cameras for RTSP streams (slow)")

	if err := fs.Parse(os.Args[2:]); err != nil {
		slog.Error("failed to parse flags", "error", err)
		os.Exit(1)
	}

	fmt.Println("Discovering ONVIF cameras on the local network...")
	fmt.Println()

	cameras, err := camera.DiscoverCameras(*timeout)
	if err != nil {
		slog.Error("discovery failed", "error", err)
		os.Exit(1)
	}

	if len(cameras) == 0 {
		fmt.Println("No cameras found.")
		return
	}

	if *yamlOutput {
		fmt.Print(camera.GenerateConfig(cameras))
		return
	}

	// Print results as a table
	fmt.Printf("Found %d camera(s):\n\n", len(cameras))
	fmt.Printf("%-16s %-20s %-15s %-15s %s\n", "IP", "NAME", "MANUFACTURER", "MODEL", "XADDRS")
	fmt.Println(strings.Repeat("-", 90))

	for _, cam := range cameras {
		xaddrs := ""
		if len(cam.XAddrs) > 0 {
			xaddrs = cam.XAddrs[0]
		}
		fmt.Printf("%-16s %-20s %-15s %-15s %s\n",
			cam.IP,
			truncate(cam.Name, 20),
			truncate(cam.Manufacturer, 15),
			truncate(cam.Model, 15),
			xaddrs,
		)
	}

	if *probeRTSP {
		fmt.Println()
		fmt.Println("Probing RTSP streams...")
		fmt.Println()

		for _, cam := range cameras {
			profiles, err := camera.ProbeRTSPForBrand(cam.IP, cam.Port, cam.Manufacturer)
			if err != nil {
				fmt.Printf("  %s: error probing - %v\n", cam.IP, err)
				continue
			}
			if len(profiles) == 0 {
				fmt.Printf("  %s: no RTSP streams found (credentials may be required)\n", cam.IP)
				continue
			}
			fmt.Printf("  %s:\n", cam.IP)
			for _, p := range profiles {
				fmt.Printf("    [%s] %s\n", p.Resolution, p.URL)
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
