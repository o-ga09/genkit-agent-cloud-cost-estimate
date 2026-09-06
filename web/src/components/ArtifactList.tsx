import type { ArtifactInfo, Line } from '../types'

const FORMAT_LABELS: Record<string, string> = {
  ir: 'IR (JSON)',
  svg: '構成図 (SVG)',
  png: '構成図 (PNG)',
  drawio: '構成図 (drawio XML)',
  xlsx: '見積もり (Excel)',
}

interface Props {
  region: string
  lines: Line[]
  failedLines: number
  artifacts: ArtifactInfo[]
  permalink: string
}

// ArtifactList は生成された成果物のダウンロードリンクと明細を出す（FR-WEB-2）。
// パーマリンクは IR の保存先を指し、LLM を経由せず再生成できる（FR-WEB-3）。
export function ArtifactList({ region, lines, failedLines, artifacts, permalink }: Props) {
  return (
    <div className="artifact-list">
      <h3>成果物（リージョン: {region}）</h3>
      {failedLines > 0 && (
        <p className="warning">⚠ {failedLines} 行は単価を取得できず、未計上のまま出力されています。</p>
      )}
      <ul className="downloads">
        {artifacts.map((a) => (
          <li key={a.format}>
            <a href={a.downloadUrl}>{FORMAT_LABELS[a.format] ?? a.format} をダウンロード</a>
          </li>
        ))}
      </ul>

      <details>
        <summary>見積もり明細（{lines.length} 行）</summary>
        <table>
          <thead>
            <tr>
              <th>リソース</th>
              <th>サービス</th>
              <th>driver</th>
              <th>数量</th>
              <th>単価</th>
              <th>SKU</th>
            </tr>
          </thead>
          <tbody>
            {lines.map((l, i) => (
              <tr key={`${l.resourceId}-${l.driverId}-${i}`}>
                <td>{l.resourceId}</td>
                <td>{l.service}</td>
                <td>{l.driverId}</td>
                <td>
                  {l.quantity} {l.unit}
                </td>
                <td>
                  {l.error ? (
                    <span className="error">取得失敗: {l.error}</span>
                  ) : (
                    `${l.unitPrice} ${l.currency}`
                  )}
                </td>
                <td>{l.sku ?? '-'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </details>

      <p className="permalink">
        パーマリンク: <a href={permalink}>{permalink}</a>
        <br />
        <small>このリンクを開くと、LLM を使わず同じ構成から再レンダリング・再見積もりします。</small>
      </p>
    </div>
  )
}
