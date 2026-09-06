import { useEffect, useState } from 'react'
import './App.css'
import { ApiError, loadArchitecture, runEstimate, saveArchitecture, sendAnswers, sendMessage, startSession } from './api'
import { ArchitectureSummary } from './components/ArchitectureSummary'
import { ArtifactList } from './components/ArtifactList'
import { ChoiceForm } from './components/ChoiceForm'
import type { Answer, Architecture, EstimateResult, Turn } from './types'

type Phase = 'intro' | 'chat' | 'result'

function describeError(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return String(e)
}

function App() {
  const [phase, setPhase] = useState<Phase>('intro')
  const [turn, setTurn] = useState<Turn | null>(null)
  const [architectureId, setArchitectureId] = useState<string | null>(null)
  const [architecture, setArchitecture] = useState<Architecture | null>(null)
  const [estimate, setEstimate] = useState<EstimateResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [input, setInput] = useState('')
  const [followUp, setFollowUp] = useState('')

  // パーマリンク（?id=...）を開いたときは、チャットを飛ばして
  // 保存済み構成から直接再レンダリング・再見積もりする（FR-WEB-3・LLM 不使用）。
  useEffect(() => {
    const id = new URLSearchParams(window.location.search).get('id')
    if (!id) return
    setLoading(true)
    loadArchitecture(id)
      .then(async (rec) => {
        setArchitecture(rec.architecture)
        setArchitectureId(rec.id)
        const est = await runEstimate(rec.id)
        setEstimate(est)
        setPhase('result')
      })
      .catch((e) => setError(describeError(e)))
      .finally(() => setLoading(false))
  }, [])

  async function guard(action: () => Promise<void>) {
    setLoading(true)
    setError(null)
    try {
      await action()
    } catch (e) {
      setError(describeError(e))
    } finally {
      setLoading(false)
    }
  }

  function handleStart() {
    const message = input.trim()
    if (!message) return
    guard(async () => {
      const next = await startSession(message)
      setTurn(next)
      setPhase('chat')
      setInput('')
    })
  }

  function handleAnswers(answers: Record<string, Answer>) {
    if (!turn) return
    guard(async () => {
      const next = await sendAnswers(turn.sessionId, answers)
      setTurn(next)
    })
  }

  function handleFollowUp() {
    if (!turn || !followUp.trim()) return
    const message = followUp.trim()
    guard(async () => {
      const next = await sendMessage(turn.sessionId, message)
      setTurn(next)
      setFollowUp('')
    })
  }

  // 利用者が構成案を確認したうえで、はじめて成果物を作る（FR-CHT-5）。
  function handleConfirm() {
    if (!turn?.architecture) return
    const architectureToSave = turn.architecture
    guard(async () => {
      const saved = await saveArchitecture(architectureToSave)
      const est = await runEstimate(saved.id)
      setArchitecture(saved.architecture)
      setArchitectureId(saved.id)
      setEstimate(est)
      setPhase('result')
      const url = new URL(window.location.href)
      url.searchParams.set('id', saved.id)
      window.history.pushState({}, '', url)
    })
  }

  function handleRegenerate() {
    if (!architectureId) return
    guard(async () => {
      const est = await runEstimate(architectureId)
      setEstimate(est)
    })
  }

  function handleReset() {
    setPhase('intro')
    setTurn(null)
    setArchitecture(null)
    setArchitectureId(null)
    setEstimate(null)
    setError(null)
    const url = new URL(window.location.href)
    url.searchParams.delete('id')
    window.history.pushState({}, '', url)
  }

  return (
    <div className="app">
      <header className="app-header">
        <h1>クラウドコスト見積もりエージェント</h1>
        <p className="subtitle">
          チャットで AWS 構成を相談し、確認のうえで構成図と見積もり Excel を生成します。
          金額と座標は LLM に出させず、決定的なコードが計算します。
        </p>
      </header>

      {error && <div className="banner error-banner">{error}</div>}
      {loading && <div className="banner loading-banner">処理中…</div>}

      {phase === 'intro' && (
        <section className="panel">
          <label htmlFor="intro-input">作りたいシステムを説明してください</label>
          <textarea
            id="intro-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="例: EC2 で動く Web アプリと RDS、S3 に画像を保存する構成"
            rows={4}
          />
          <button onClick={handleStart} disabled={loading || !input.trim()}>
            相談を始める
          </button>
        </section>
      )}

      {phase === 'chat' && turn && (
        <section className="panel">
          {turn.message && <p className="assistant-message">{turn.message}</p>}

          {turn.status === 'asking' && turn.questions && turn.questions.length > 0 && (
            <ChoiceForm questions={turn.questions} onSubmit={handleAnswers} disabled={loading} />
          )}

          {turn.status === 'proposed' && turn.architecture && (
            <>
              <ArchitectureSummary architecture={turn.architecture} />
              <div className="actions">
                <button onClick={handleConfirm} disabled={loading}>
                  この内容で生成する
                </button>
              </div>
              <div className="follow-up">
                <label htmlFor="follow-up-input">修正したい点があれば伝えてください</label>
                <div className="follow-up-row">
                  <input
                    id="follow-up-input"
                    value={followUp}
                    onChange={(e) => setFollowUp(e.target.value)}
                    placeholder="例: RDS を Multi-AZ にして"
                  />
                  <button onClick={handleFollowUp} disabled={loading || !followUp.trim()}>
                    送信
                  </button>
                </div>
              </div>
            </>
          )}
        </section>
      )}

      {phase === 'result' && architecture && estimate && (
        <section className="panel">
          <ArchitectureSummary architecture={architecture} />
          <ArtifactList
            region={estimate.region}
            lines={estimate.lines}
            failedLines={estimate.failedLines}
            artifacts={estimate.artifacts}
            permalink={`${window.location.origin}${window.location.pathname}?id=${architectureId}`}
          />
          <div className="actions">
            <button onClick={handleRegenerate} disabled={loading}>
              単価を取り直して再生成
            </button>
            <button onClick={handleReset} disabled={loading}>
              新しい相談を始める
            </button>
          </div>
        </section>
      )}
    </div>
  )
}

export default App
