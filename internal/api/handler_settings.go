package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/rvben/vedetta/internal/config"
	"github.com/rvben/vedetta/internal/mqtt"
)

func (s *Server) GetMQTTSettings(w http.ResponseWriter, _ *http.Request) {
	status := "disabled"
	if s.mqttClient != nil {
		status = "connected"
	} else if s.mqttEnabled {
		status = "disconnected"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  s.mqttConfig.Enabled,
		"host":     s.mqttConfig.Host,
		"port":     s.mqttConfig.Port,
		"username": s.mqttConfig.Username,
		"topic":    s.mqttConfig.Topic,
		"status":   status,
	})
}

func (s *Server) UpdateMQTTSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req struct {
		Enabled  bool   `json:"enabled"`
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		Topic    string `json:"topic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	if req.Port < 1 || req.Port > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "port must be between 1 and 65535"})
		return
	}

	mqttCfg := config.MQTTConfig{
		Enabled:  req.Enabled,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		Topic:    req.Topic,
	}

	if err := config.UpdateMQTT(s.configPath, mqttCfg); err != nil {
		slog.Error("failed to write MQTT config", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save config"})
		return
	}

	s.reconnectMQTT(mqttCfg)

	status := "disabled"
	if s.mqttClient != nil {
		status = "connected"
	} else if s.mqttEnabled {
		status = "disconnected"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":  mqttCfg.Enabled,
		"host":     mqttCfg.Host,
		"port":     mqttCfg.Port,
		"username": mqttCfg.Username,
		"topic":    mqttCfg.Topic,
		"status":   status,
	})
}

func (s *Server) TestMQTTConnection(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	defer r.Body.Close()

	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	cfg := config.MQTTConfig{
		Enabled:  true,
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
	}

	client, err := mqtt.New(cfg)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	client.Close()

	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) DiscoverMQTTBrokers(w http.ResponseWriter, _ *http.Request) {
	brokers, err := mqtt.DiscoverBrokers(3 * time.Second)
	if err != nil {
		slog.Warn("MQTT broker discovery failed", "error", err)
		brokers = []mqtt.Broker{}
	}
	if brokers == nil {
		brokers = []mqtt.Broker{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"brokers": brokers})
}

func (s *Server) GetUpdateStatus(w http.ResponseWriter, _ *http.Request) {
	if s.updateChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":          s.version,
			"update_available": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, s.updateChecker.Status())
}

func (s *Server) CheckForUpdates(w http.ResponseWriter, _ *http.Request) {
	if s.updateChecker == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":          s.version,
			"update_available": false,
		})
		return
	}
	status := s.updateChecker.CheckNow()
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) DismissUpdate(w http.ResponseWriter, _ *http.Request) {
	if s.updateChecker == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.updateChecker.Dismiss(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to dismiss"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reconnectMQTT(cfg config.MQTTConfig) {
	if s.mqttClient != nil {
		if closer, ok := s.mqttClient.(interface{ Close() }); ok {
			closer.Close()
		}
		s.mqttClient = nil
	}

	s.mqttConfig = cfg
	s.mqttEnabled = cfg.Enabled

	if !cfg.Enabled {
		return
	}

	client, err := mqtt.New(cfg)
	if err != nil {
		slog.Warn("MQTT reconnect failed", "error", err)
		return
	}
	s.mqttClient = client
}
