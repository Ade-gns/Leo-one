/**
 * ScriptFormModal.tsx — Création ou modification d'un script de la bibliothèque
 */
import { useRef, useState } from 'react'
import { X, FileCode, Loader2, Save, Upload } from 'lucide-react'
import { useCreateScript, useUpdateScript } from '@/hooks/useScripts'
import { cn } from '@/lib/utils'
import type { Script, ScriptInterpreter } from '@/types/script'

const INTERPRETERS: { value: ScriptInterpreter; label: string }[] = [
  { value: 'bash',       label: 'Bash'       },
  { value: 'sh',         label: 'Sh'         },
  { value: 'python',     label: 'Python'     },
  { value: 'cmd',        label: 'CMD'        },
  { value: 'powershell', label: 'PowerShell' },
]

/** Déduit l'interpréteur à partir de l'extension du fichier importé — best
 *  effort, l'utilisateur reste libre de corriger via les boutons. */
function interpreterFromFilename(filename: string): ScriptInterpreter | null {
  const ext = filename.slice(filename.lastIndexOf('.') + 1).toLowerCase()
  switch (ext) {
    case 'ps1':          return 'powershell'
    case 'bat':
    case 'cmd':           return 'cmd'
    case 'py':            return 'python'
    case 'sh':             return 'bash'
    default:               return null
  }
}

interface ScriptFormModalProps {
  script?: Script  // présent = édition, absent = création
  onClose: () => void
}

export function ScriptFormModal({ script, onClose }: ScriptFormModalProps) {
  const isEdit = !!script

  const [name, setName]               = useState(script?.name ?? '')
  const [description, setDescription] = useState(script?.description ?? '')
  const [interpreter, setInterpreter] = useState<ScriptInterpreter>(script?.interpreter ?? 'bash')
  const [content, setContent]         = useState(script?.content ?? '')
  const [error, setError]             = useState<string | null>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  const createScript = useCreateScript()
  const updateScript = useUpdateScript()
  const isPending = createScript.isPending || updateScript.isPending

  const canSubmit = name.trim() !== '' && content.trim() !== ''

  const handleFileSelect = (file: File | undefined) => {
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      setContent(typeof reader.result === 'string' ? reader.result : '')
      const guessed = interpreterFromFilename(file.name)
      if (guessed) setInterpreter(guessed)
      if (!name.trim()) {
        setName(file.name.replace(/\.[^.]+$/, ''))
      }
    }
    reader.readAsText(file)
  }

  const handleSubmit = () => {
    if (!canSubmit) return
    setError(null)

    const payload = {
      name: name.trim(),
      description: description.trim() || undefined,
      interpreter,
      content,
    }
    const onError = (err: unknown) => setError(err instanceof Error ? err.message : 'Erreur inconnue')

    if (isEdit) {
      updateScript.mutate({ scriptID: script.id, payload }, { onSuccess: onClose, onError })
    } else {
      createScript.mutate(payload, { onSuccess: onClose, onError })
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4 backdrop-blur-sm">
      <div className="flex w-full max-w-2xl flex-col rounded-2xl bg-white shadow-2xl">

        <div className="flex items-center justify-between border-b border-gray-100 px-6 py-4">
          <div className="flex items-center gap-3">
            <FileCode className="h-5 w-5 text-brand-600" />
            <h2 className="text-base font-semibold text-gray-900">
              {isEdit ? 'Modifier le script' : 'Nouveau script'}
            </h2>
          </div>
          <button onClick={onClose} className="rounded-lg p-1.5 text-gray-400 hover:bg-gray-100">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="flex flex-col gap-4 p-6">
          <div className="flex gap-4">
            <div className="flex-1">
              <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Nom
              </label>
              <input
                type="text"
                value={name}
                onChange={e => setName(e.target.value)}
                placeholder="ex : Nettoyage disque"
                className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Interpréteur
              </label>
              <div className="flex gap-1">
                {INTERPRETERS.map(i => (
                  <button
                    key={i.value}
                    onClick={() => setInterpreter(i.value)}
                    className={cn(
                      'rounded-md px-2.5 py-2 text-xs font-medium transition-colors',
                      interpreter === i.value
                        ? 'bg-brand-900 text-white'
                        : 'bg-gray-100 text-gray-600 hover:bg-gray-200',
                    )}
                  >
                    {i.label}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div>
            <label className="mb-1.5 block text-xs font-semibold uppercase tracking-wider text-gray-400">
              Description (optionnel)
            </label>
            <input
              type="text"
              value={description}
              onChange={e => setDescription(e.target.value)}
              className="w-full rounded-lg border border-gray-200 px-3 py-2 text-sm outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          <div>
            <div className="mb-1.5 flex items-center justify-between">
              <label className="block text-xs font-semibold uppercase tracking-wider text-gray-400">
                Contenu
              </label>
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-brand-600 hover:bg-brand-50"
              >
                <Upload className="h-3.5 w-3.5" />
                Importer un fichier
              </button>
              <input
                ref={fileInputRef}
                type="file"
                accept=".sh,.bash,.ps1,.py,.bat,.cmd,.txt"
                className="hidden"
                onChange={e => {
                  handleFileSelect(e.target.files?.[0])
                  e.target.value = ''
                }}
              />
            </div>
            <textarea
              value={content}
              onChange={e => setContent(e.target.value)}
              rows={10}
              className="w-full resize-none rounded-lg border border-gray-200 bg-gray-950 px-4 py-3 font-mono text-sm text-green-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            />
          </div>

          {error && <p className="text-xs text-red-500">Erreur : {error}</p>}

          <div className="flex justify-end">
            <button
              onClick={handleSubmit}
              disabled={!canSubmit || isPending}
              className="flex items-center gap-2 rounded-lg bg-brand-900 px-5 py-2.5 text-sm font-semibold text-white hover:bg-brand-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {isPending
                ? <><Loader2 className="h-4 w-4 animate-spin" />Enregistrement…</>
                : <><Save className="h-4 w-4" />{isEdit ? 'Enregistrer' : 'Créer'}</>
              }
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
