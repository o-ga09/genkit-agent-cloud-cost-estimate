import { useState } from 'react'
import type { Answer, Question } from '../types'

interface Props {
  questions: Question[]
  onSubmit: (answers: Record<string, Answer>) => void
  disabled?: boolean
}

// ChoiceForm は intake agent の ask_user ツール（tool interrupt）が
// 返してきた選択肢を描画し、回答をまとめて返す（FR-CHT-1 / FR-CHT-2）。
export function ChoiceForm({ questions, onSubmit, disabled }: Props) {
  const [selected, setSelected] = useState<Record<string, string[]>>({})

  function toggle(questionId: string, option: string, multi: boolean) {
    setSelected((prev) => {
      const current = prev[questionId] ?? []
      if (multi) {
        const next = current.includes(option)
          ? current.filter((o) => o !== option)
          : [...current, option]
        return { ...prev, [questionId]: next }
      }
      return { ...prev, [questionId]: [option] }
    })
  }

  function submit() {
    const answers: Record<string, Answer> = {}
    for (const q of questions) {
      answers[q.id] = { selected: selected[q.id] ?? [] }
    }
    onSubmit(answers)
  }

  const allAnswered = questions.every((q) => (selected[q.id]?.length ?? 0) > 0)

  return (
    <div className="choice-form">
      {questions.map((q) => (
        <fieldset key={q.id} className="choice-question">
          <legend>{q.choice.question}</legend>
          {q.choice.options.map((opt) => (
            <label key={opt} className="choice-option">
              <input
                type={q.choice.multi ? 'checkbox' : 'radio'}
                name={q.id}
                checked={(selected[q.id] ?? []).includes(opt)}
                onChange={() => toggle(q.id, opt, q.choice.multi)}
                disabled={disabled}
              />
              <span>{opt}</span>
            </label>
          ))}
        </fieldset>
      ))}
      <button type="button" onClick={submit} disabled={disabled || !allAnswered}>
        回答する
      </button>
    </div>
  )
}
