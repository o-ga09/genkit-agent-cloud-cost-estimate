package webapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/labstack/echo/v5"

	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/estimateflow"
	"github.com/o-ga09/genkit-agent-cloud-cost-estimate/internal/ir"
)

type saveArchitectureRequest struct {
	Architecture json.RawMessage `json:"architecture" validate:"required"`
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
func (s *Server) handleSaveArchitecture(c *echo.Context) error {
	var req saveArchitectureRequest
	if err := bindAndValidate(c, &req); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	arch, err := ir.Decode(bytes.NewReader(req.Architecture))
	if err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	if err := arch.Validate(s.Catalog); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}

	rec := &ArchitectureRecord{ID: s.newID(), Architecture: arch, CreatedAt: s.now()}
	if err := s.Store.Save(c.Request().Context(), rec); err != nil {
		return writeError(c, http.StatusInternalServerError, err)
	}
	return writeJSON(c, http.StatusCreated, saveArchitectureResponse{ID: rec.ID, Architecture: rec.Architecture})
}

// handleLoadArchitecture は保存済み構成を返す（パーマリンクの表示用）。
func (s *Server) handleLoadArchitecture(c *echo.Context) error {
	id := c.Param("id")
	rec, err := s.Store.Load(c.Request().Context(), id)
	if err != nil {
		return writeArchitectureError(c, err)
	}
	return writeJSON(c, http.StatusOK, rec)
}

// handleEstimate は保存済み構成から成果物一式を作る（FR-WEB-3: 再見積もり）。
// 中身は LLM を通さない estimateflow.Run そのもの（NFR-3）。
func (s *Server) handleEstimate(c *echo.Context) error {
	id := c.Param("id")
	rec, err := s.Store.Load(c.Request().Context(), id)
	if err != nil {
		return writeArchitectureError(c, err)
	}

	var req estimateRequest
	if err := bindAndValidate(c, &req); err != nil {
		return writeError(c, http.StatusBadRequest, err)
	}
	formats := make([]estimateflow.Format, 0, len(req.Formats))
	for _, f := range req.Formats {
		formats = append(formats, estimateflow.Format(f))
	}

	resp, err := s.Estimator.Run(c.Request().Context(), &estimateflow.Request{
		Architecture: rec.Architecture,
		Formats:      formats,
		BaseName:     id,
	})
	if err != nil {
		return writeError(c, http.StatusInternalServerError, err)
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
	return writeJSON(c, http.StatusOK, out)
}

// handleDownloadArtifact は成果物を 1 つだけダウンロードさせる（FR-WEB-2）。
//
// 成果物のバイト列は保存せず、保存済み IR から都度作り直す。IR さえあれば
// 決定的に再生成できるため（NFR-1 / NFR-3）、キャッシュを持つ必要がない。
func (s *Server) handleDownloadArtifact(c *echo.Context) error {
	id := c.Param("id")
	format := estimateflow.Format(c.Param("format"))
	if !slices.Contains(downloadableFormats, format) {
		return writeError(c, http.StatusBadRequest, fmt.Errorf("未対応の形式です: %q", format))
	}

	rec, err := s.Store.Load(c.Request().Context(), id)
	if err != nil {
		return writeArchitectureError(c, err)
	}

	resp, err := s.Estimator.Run(c.Request().Context(), &estimateflow.Request{
		Architecture: rec.Architecture,
		Formats:      []estimateflow.Format{format},
		BaseName:     id,
	})
	if err != nil {
		return writeError(c, http.StatusInternalServerError, err)
	}
	if len(resp.Artifacts) == 0 {
		return writeError(c, http.StatusInternalServerError, fmt.Errorf("成果物を作れませんでした: %q", format))
	}
	artifact := resp.Artifacts[0]
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", artifact.Filename))
	return c.Blob(http.StatusOK, artifact.ContentType, artifact.Content)
}

func writeArchitectureError(c *echo.Context, err error) error {
	if errors.Is(err, ErrArchitectureNotFound) {
		return writeError(c, http.StatusNotFound, err)
	}
	return writeError(c, http.StatusInternalServerError, err)
}
