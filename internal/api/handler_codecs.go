package api

import (
	"errors"
	"net/http"

	"github.com/rvben/vedetta/internal/media"
)

var (
	openH264StatusInfo = media.OpenH264StatusInfo
	openH264Install    = media.InstallOpenH264
)

type openH264StatusResponse struct {
	Codec string `json:"codec"`
	media.OpenH264Status
	openH264Presentation
}

type openH264Presentation struct {
	State           string `json:"state"`
	Badge           string `json:"badge"`
	BadgeTone       string `json:"badge_tone,omitempty"`
	Headline        string `json:"headline"`
	Detail          string `json:"detail,omitempty"`
	Diagnostic      string `json:"diagnostic,omitempty"`
	ActionLabel     string `json:"action_label,omitempty"`
	ShowInstall     bool   `json:"show_install"`
	ShowDiagnostics bool   `json:"show_diagnostics"`
}

func describeOpenH264Status(status media.OpenH264Status) openH264Presentation {
	ui := openH264Presentation{
		State:       "optional",
		Badge:       "Optional",
		BadgeTone:   "muted",
		Headline:    "OpenH264 is not installed yet.",
		Detail:      "Optional. Install it to enable local H.264 decoding for detection and snapshot extraction.",
		ActionLabel: "Install OpenH264",
		ShowInstall: status.Supported,
	}

	switch {
	case status.Installing:
		ui.State = "installing"
		ui.Badge = "Installing"
		ui.BadgeTone = "info"
		ui.Headline = "Installing OpenH264…"
		ui.Detail = "Vedetta is downloading, verifying, and activating the codec."
		ui.ActionLabel = "Installing…"
		ui.ShowInstall = true
	case status.Available:
		ui.State = "ready"
		ui.Badge = "Ready"
		ui.BadgeTone = "success"
		ui.ShowInstall = false
		ui.ActionLabel = ""
		switch status.Source {
		case "installed":
			ui.Headline = "OpenH264 installed and ready."
			ui.Detail = "Vedetta will use the verified installed codec for local H.264 decoding."
		case "system":
			ui.Headline = "OpenH264 already available."
			ui.Detail = "A system OpenH264 library is already installed, so no additional setup is needed."
		case "environment":
			ui.Headline = "OpenH264 already available."
			ui.Detail = "Vedetta is using the library provided by OPENH264_LIB."
		default:
			ui.Headline = "OpenH264 ready."
			ui.Detail = "Vedetta can use OpenH264 for local H.264 decoding."
		}
	case !status.Supported:
		ui.State = "unsupported"
		ui.Badge = "Unavailable"
		ui.BadgeTone = "muted"
		ui.Headline = "OpenH264 install is not available on this platform."
		ui.Detail = "You can continue setup without it."
		ui.ActionLabel = ""
		ui.ShowInstall = false
		if status.Error != "" {
			ui.Diagnostic = status.Error
			ui.ShowDiagnostics = true
		}
	case status.Installed:
		ui.State = "attention"
		ui.Badge = "Needs Attention"
		ui.BadgeTone = "warning"
		ui.Headline = "OpenH264 is installed but not ready."
		ui.Detail = "Vedetta could not load the installed codec. Reinstall it or use a system library instead."
		ui.ActionLabel = "Reinstall OpenH264"
		ui.ShowInstall = status.Supported
		if status.Error != "" {
			ui.Diagnostic = status.Error
			ui.ShowDiagnostics = true
		}
	case status.Error != "":
		ui.State = "install_failed"
		ui.Badge = "Install Failed"
		ui.BadgeTone = "error"
		ui.Headline = "OpenH264 install failed."
		ui.Detail = "You can try again, or continue setup without it."
		ui.ActionLabel = "Try Again"
		ui.ShowInstall = status.Supported
		ui.Diagnostic = status.Error
		ui.ShowDiagnostics = true
	}

	return ui
}

func openH264StatusResponseFor(status media.OpenH264Status) openH264StatusResponse {
	return openH264StatusResponse{
		Codec:                "openh264",
		OpenH264Status:       status,
		openH264Presentation: describeOpenH264Status(status),
	}
}

func respondOpenH264Status(w http.ResponseWriter, statusCode int, status media.OpenH264Status) {
	writeJSON(w, statusCode, openH264StatusResponseFor(status))
}

func openH264InstallStatusCode(status media.OpenH264Status, err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, media.ErrOpenH264InstallInProgress):
		return http.StatusConflict
	case !status.Supported:
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func installOpenH264AndRespond(w http.ResponseWriter, r *http.Request) {
	status, err := openH264Install(r.Context())
	if err != nil && status.Error == "" {
		status.Error = err.Error()
	}
	respondOpenH264Status(w, openH264InstallStatusCode(status, err), status)
}

func (s *Server) GetOpenH264Status(w http.ResponseWriter, _ *http.Request) {
	respondOpenH264Status(w, http.StatusOK, openH264StatusInfo())
}

func (s *Server) InstallOpenH264(w http.ResponseWriter, r *http.Request) {
	installOpenH264AndRespond(w, r)
}

func (h *SetupHandler) HandleOpenH264Status(w http.ResponseWriter, _ *http.Request) {
	respondOpenH264Status(w, http.StatusOK, openH264StatusInfo())
}

func (h *SetupHandler) HandleInstallOpenH264(w http.ResponseWriter, r *http.Request) {
	installOpenH264AndRespond(w, r)
}
