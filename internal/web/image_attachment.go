package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const imageAttachmentMaxBytes = 2 << 20

type imageAttachmentRequest struct {
	MIME string `json:"mime"`
	Data string `json:"data"`
}

type imageAttachmentResponse struct {
	Path   string `json:"path"`
	MIME   string `json:"mime"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var (
	errImageAttachmentDirUnavailable = errors.New("image attachment dir unavailable")
	errImageAttachmentDataRequired   = errors.New("image data required")
	errImageAttachmentTooLarge       = errors.New("image exceeds 2MB")
	errImageAttachmentUnsupported    = errors.New("unsupported image type")
	errImageAttachmentUnsafePath     = errors.New("unsafe image attachment path")
	errImageAttachmentCollision      = errors.New("image attachment hash collision")
)

func (s *Server) handleImageAttachment(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ownerSessionID, ok := s.requireHTTPAuth(w, r)
	if !ok {
		return
	}
	if !s.requireCSRF(w, r, ownerSessionID) {
		return
	}
	var req imageAttachmentRequest
	if !s.readJSONBodyLimit(w, r, ownerSessionID, &req, imageAttachmentBodyLimit()) {
		return
	}
	resp, err := s.storeImageAttachment(req)
	if err != nil {
		status := imageAttachmentErrorStatus(err)
		s.log.Warn("web image attachment failed", "request_id", requestID(r), "mime", req.MIME, "err", err)
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) storeImageAttachment(req imageAttachmentRequest) (imageAttachmentResponse, error) {
	raw := strings.TrimSpace(req.Data)
	if raw == "" {
		return imageAttachmentResponse{}, errImageAttachmentDataRequired
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(data) == 0 {
		return imageAttachmentResponse{}, errImageAttachmentDataRequired
	}
	if len(data) > imageAttachmentMaxBytes {
		return imageAttachmentResponse{}, errImageAttachmentTooLarge
	}
	mime, ext, err := validateImageAttachmentType(req.MIME, data)
	if err != nil {
		return imageAttachmentResponse{}, err
	}
	root, err := s.imageAttachmentRoot()
	if err != nil {
		return imageAttachmentResponse{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return imageAttachmentResponse{}, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	abs := filepath.Join(root, hash+ext)
	if err := writeImageAttachment(abs, data); err != nil {
		return imageAttachmentResponse{}, err
	}
	return imageAttachmentResponse{Path: abs, MIME: mime, Size: int64(len(data)), SHA256: hash}, nil
}

func (s *Server) imageAttachmentRoot() (string, error) {
	if root := strings.TrimSpace(s.uploadDir); root != "" {
		return filepath.Abs(root)
	}
	if s.db != nil && strings.TrimSpace(s.db.Path()) != "" {
		return filepath.Join(filepath.Dir(s.db.Path()), "uploads"), nil
	}
	return "", errImageAttachmentDirUnavailable
}

func validateImageAttachmentType(declared string, data []byte) (string, string, error) {
	declared = strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	if declared == "image/jpg" {
		declared = "image/jpeg"
	}
	if declared != "" && !supportedImageAttachmentMIME(declared) {
		return "", "", errImageAttachmentUnsupported
	}
	detected, ext := detectImageAttachmentType(data)
	if detected == "" || declared != "" && declared != detected {
		return "", "", errImageAttachmentUnsupported
	}
	return detected, ext, nil
}

func detectImageAttachmentType(data []byte) (string, string) {
	if len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "image/png", ".png"
	}
	if len(data) >= 3 && data[0] == 0xff && data[1] == 0xd8 && data[2] == 0xff {
		return "image/jpeg", ".jpg"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp", ".webp"
	}
	return "", ""
}

func supportedImageAttachmentMIME(mime string) bool {
	return mime == "image/png" || mime == "image/jpeg" || mime == "image/webp"
}

func writeImageAttachment(abs string, data []byte) error {
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		if _, writeErr := f.Write(data); writeErr != nil {
			_ = f.Close()
			_ = os.Remove(abs)
			return writeErr
		}
		if closeErr := f.Close(); closeErr != nil {
			_ = os.Remove(abs)
			return closeErr
		}
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return errImageAttachmentUnsafePath
	}
	existing, err := os.ReadFile(abs)
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, data) {
		return errImageAttachmentCollision
	}
	return nil
}

func imageAttachmentBodyLimit() int64 {
	return int64(((imageAttachmentMaxBytes + 2) / 3 * 4) + (64 << 10))
}

func imageAttachmentErrorStatus(err error) int {
	switch {
	case errors.Is(err, errImageAttachmentDataRequired), errors.Is(err, errImageAttachmentUnsafePath):
		return http.StatusBadRequest
	case errors.Is(err, errImageAttachmentUnsupported):
		return http.StatusUnsupportedMediaType
	case errors.Is(err, errImageAttachmentTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, errImageAttachmentDirUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
