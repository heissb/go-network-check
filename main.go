package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Device struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	LastSeen string `json:"last_seen"`
}

type NetworkStatus struct {
	LocalIP    string   `json:"local_ip"`
	Subnet     string   `json:"subnet"`
	DeviceCount int     `json:"device_count"`
	Devices    []Device `json:"devices"`
}

func main() {
	http.HandleFunc("/api/network/status", handleNetworkStatus)
	http.HandleFunc("/api/device/ping", handleDevicePing)
	http.HandleFunc("/api/network/scan", handleNetworkScan)
	http.HandleFunc("/", handleRoot)

	fmt.Println("Starting Network Status API on :8080")
	fmt.Println("Endpoints:")
	fmt.Println("  GET  /api/network/status - Get quick network overview")
	fmt.Println("  GET  /api/network/scan   - Scan network for devices")
	fmt.Println("  POST /api/device/ping    - Ping specific device (JSON: {\"ip\": \"192.168.1.1\"})")
	
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Printf("Error starting server: %v\n", err)
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	html := `
	<html>
	<head><title>Network Status API</title></head>
	<body>
		<h1>Network Status API</h1>
		<ul>
			<li><a href="/api/network/status">GET /api/network/status</a> - Quick network overview</li>
			<li><a href="/api/network/scan">GET /api/network/scan</a> - Full network scan (may take time)</li>
			<li>POST /api/device/ping - Ping specific device</li>
		</ul>
	</body>
	</html>
	`
	w.Write([]byte(html))
}

func handleNetworkStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	localIP, err := getLocalIP()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get local IP")
		return
	}

	subnet := getSubnet(localIP)
	
	status := NetworkStatus{
		LocalIP:     localIP,
		Subnet:      subnet,
		DeviceCount: 0,
		Devices:     []Device{},
	}

	// Add local device
	hostname, _ := net.LookupAddr(localIP)
	hostnameStr := localIP
	if len(hostname) > 0 {
		hostnameStr = hostname[0]
	}

	status.Devices = append(status.Devices, Device{
		IP:       localIP,
		Hostname: hostnameStr,
		Status:   "online",
		LastSeen: time.Now().Format(time.RFC3339),
	})
	status.DeviceCount = 1

	json.NewEncoder(w).Encode(status)
}

func handleNetworkScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	localIP, err := getLocalIP()
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to get local IP")
		return
	}

	subnet := getSubnet(localIP)
	devices := scanNetwork(localIP)

	status := NetworkStatus{
		LocalIP:     localIP,
		Subnet:      subnet,
		DeviceCount: len(devices),
		Devices:     devices,
	}

	json.NewEncoder(w).Encode(status)
}

func handleDevicePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		IP string `json:"ip"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.IP == "" {
		respondWithError(w, http.StatusBadRequest, "IP address is required")
		return
	}

	device := pingDevice(req.IP)
	json.NewEncoder(w).Encode(device)
}

func getLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no local IP found")
}

func getSubnet(ip string) string {
	parts := strings.Split(ip, ".")
	if len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.0/24", parts[0], parts[1], parts[2])
	}
	return ""
}

func scanNetwork(localIP string) []Device {
	parts := strings.Split(localIP, ".")
	if len(parts) != 4 {
		return []Device{}
	}

	baseIP := fmt.Sprintf("%s.%s.%s", parts[0], parts[1], parts[2])
	devices := []Device{}
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Scan first 50 IPs for reasonable performance
	for i := 1; i <= 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ip := fmt.Sprintf("%s.%d", baseIP, i)
			device := pingDevice(ip)
			if device.Status == "online" {
				mu.Lock()
				devices = append(devices, device)
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	return devices
}

func pingDevice(ip string) Device {
	device := Device{
		IP:       ip,
		Hostname: ip,
		Status:   "offline",
		LastSeen: "",
	}

	// Try to connect with a short timeout
	conn, err := net.DialTimeout("tcp", ip+":80", 500*time.Millisecond)
	if err == nil {
		conn.Close()
		device.Status = "online"
		device.LastSeen = time.Now().Format(time.RFC3339)
	} else {
		// Try ICMP-style check via UDP
		conn, err := net.DialTimeout("udp", ip+":53", 500*time.Millisecond)
		if err == nil {
			conn.Close()
			device.Status = "online"
			device.LastSeen = time.Now().Format(time.RFC3339)
		}
	}

	if device.Status == "online" {
		// Try to resolve hostname
		hostnames, err := net.LookupAddr(ip)
		if err == nil && len(hostnames) > 0 {
			device.Hostname = hostnames[0]
		}
	}

	return device
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
