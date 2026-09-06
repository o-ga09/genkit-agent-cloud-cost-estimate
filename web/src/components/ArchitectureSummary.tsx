import type { Architecture } from '../types'

// ArchitectureSummary は構成案と前提条件を提示する。
// 金額は一切出さない — 単価の取得と計算は生成（estimate）を押してから行われる（PRIN-1）。
export function ArchitectureSummary({ architecture }: { architecture: Architecture }) {
  const a = architecture.assumptions
  return (
    <div className="architecture-summary">
      <h3>
        構成案（{architecture.provider} / {architecture.region}）
      </h3>
      <table>
        <thead>
          <tr>
            <th>ID</th>
            <th>サービス</th>
            <th>ラベル</th>
            <th>パラメータ</th>
          </tr>
        </thead>
        <tbody>
          {architecture.resources.map((r) => (
            <tr key={r.id}>
              <td>{r.id}</td>
              <td>{r.service}</td>
              <td>{r.label ?? '-'}</td>
              <td className="params">{r.params ? JSON.stringify(r.params) : '-'}</td>
            </tr>
          ))}
        </tbody>
      </table>
      <p className="assumptions">
        稼働 {a.hoursPerDay} 時間/日 × {a.daysPerMonth} 日/月・月間リクエスト{' '}
        {a.requestsPerMonth.toLocaleString()}・為替 {a.fxRate} 円・割引率 {a.discountRate}
      </p>
      <p className="note">金額はまだ出ていません。単価の取得と計算はこのあとの生成で行います。</p>
    </div>
  )
}
