package webapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

type saveArchitectureRequest struct {
	Architecture json.RawMessage `json:"architecture"`
}

type saveArchitectureResponse struct {
	ID           string           `json:"id"`
	Architecture *ir.Architecture `json:"architecture"`
}

type estimateRequest struct {
	// Formats は作る成果物。空なら estimateflow.DefaultFormats。
	Formats []string `json:"formats,omitempty"`
}

type artifactDTO struct {
	Format      string `json:"format"`
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	DownloadURL string `json:"downloadUrl"`
}

type estimateResponse struct {
	Region      string              `json:"region"`
	Lines       []estimateflow.Line `json:"lines"`
	FailedLines int                 `json:"failedLines"`
	Artifacts   []artifactDTO       `json:"artifacts"`
}

// downloadableFormats は GET .../artifacts/{format} で配布してよい形式。
var downloadableFormats = []estimateflow.Format{
	estimateflow.FormatIR,
	estimateflow.FormatSVG,
	estimateflow.FormatPNG,
	estimateflow.FormatDrawio,
	estimateflow.FormatXLSX,
}

// handleSaveArchitecture は構成案を保存する（FR-WEB-3）。
//
// 保存の前に catalog でバリデーションする。ここを通れば
// 生成物づくりに使ってよい構成であることが保証される。
func (s *Server) handleSaveArchitecture(w http.ResponseWriter, r *http.Request) {
	var req saveArchitectureRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	arch, err := ir.Decode(bytes.NewReader(req.Architecture))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := arch.Validate(s.Catalog); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	rec := &ArchitectureRecord{ID: s.newID(), Architecture: arch, CreatedAt: s.now()}
	if err := s.Store.Save(r.Context(), rec); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, saveArchitectureResponse{ID: rec.ID, Architecture: rec.Architecture})
}

// handleLoadArchitecture は保存済み構成を返す（パーマリンクの表示用）。
func (s *Server) handleLoadArchitecture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.Store.Load(r.Context(), id)
	if err != nil {
		writeArchitectureError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// handleEstimate は保存済み構成から成果物一式を作る（FR-WEB-3: 再見積もり）。
// 中身は LLM を通さない estimateflow.Run そのもの（NFR-3）。
func (s *Server) handleEstimate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rec, err := s.Store.Load(r.Context(), id)
	if err != nil {
		writeArchitectureError(w, err)
		return
	}

	var req estimateRequest
	if r.ContentLength != 0 {
		if err := decodeBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	formats := make([]estimateflow.Format, 0, len(req.Formats))
	for _, f := range req.Formats {
		formats = append(formats, estimateflow.Format(f))
	}

	resp, err := s.Estimator.Run(r.Context(), &estimateflow.Request{
		Architecture: rec.Architecture,
		Formats:      formats,
		BaseName:     id,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}

	out := estimateResponse{Region: resp.Region, Lines: resp.Lines, FailedLines: resp.FailedLines}
	for _, a := range resp.Artifacts {
		out.Artifacts = append(out.Artifacts, artifactDTO{
			Format:      string(a.Format),
			Filename:    a.Filename,
			ContentType: a.ContentType,
			DownloadURL: fmt.Sprintf("/api/architectures/%s/artifacts/%s", id, a.Format),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleDownloadArtifact は成果物を 1 つだけダウンロードさせる（FR-WEB-2）。
//
// 成果物のバイト列は保存せず、保存済み IR から都度作り直す。IR さえあれば
// 決定的に再生成できるため（NFR-1 / NFR-3）、キャッシュを持つ必要がない。
func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	format := estimateflow.Format(r.PathValue("format"))
	if !slices.Contains(downloadableFormats, format) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("未対応の形式です: %q", format))
		return
	}

	rec, err := s.Store.Load(r.Context(), id)
	if err != nil {
		writeArchitectureError(w, err)
		return
	}

	resp, err := s.Estimator.Run(r.Context(), &estimateflow.Request{
		Architecture: rec.Architecture,
		Formats:      []estimateflow.Format{format},
		BaseName:     id,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(resp.Artifacts) == 0 {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("成果物を作れませんでした: %q", format))
		return
	}
	artifact := resp.Artifacts[0]
	w.Header().Set("Content-Type", artifact.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", artifact.Filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(artifact.Content)
}

func writeArchitectureError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrArchitectureNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}
