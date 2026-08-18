import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithClient } from '@/test/renderWithClient'
import { ScriptFormModal } from './ScriptFormModal'
import type { Script } from '@/types/script'

vi.mock('@/api/scripts', () => ({
  scriptsApi: {
    list:   vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
  },
  schedulesApi: {
    list: vi.fn(),
  },
}))

import { scriptsApi } from '@/api/scripts'

const SCRIPT: Script = {
  id: 's1', name: 'Existant', description: 'desc', interpreter: 'python', content: 'print(1)',
  created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

function getNameInput() {
  return screen.getByPlaceholderText('ex : Nettoyage disque')
}
function getContentTextarea() {
  return document.querySelector('textarea') as HTMLTextAreaElement
}
function getSubmitButton(name: RegExp) {
  return screen.getByRole('button', { name })
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('ScriptFormModal — création', () => {
  it('désactive la soumission tant que nom et contenu ne sont pas remplis', () => {
    renderWithClient(<ScriptFormModal onClose={vi.fn()} />)

    expect(getSubmitButton(/créer/i)).toBeDisabled()
  })

  it('crée le script avec le payload attendu et ferme la modale au succès', async () => {
    vi.mocked(scriptsApi.create).mockResolvedValue({ data: SCRIPT })
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScriptFormModal onClose={onClose} />)

    await user.type(getNameInput(), 'Mon script')
    await user.type(getContentTextarea(), 'echo hi')

    const submit = getSubmitButton(/créer/i)
    expect(submit).toBeEnabled()
    await user.click(submit)

    await waitFor(() => expect(scriptsApi.create).toHaveBeenCalledWith({
      name: 'Mon script', description: undefined, interpreter: 'bash', content: 'echo hi',
    }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('affiche un état de chargement pendant la création', async () => {
    let resolveCreate: (v: { data: Script }) => void = () => {}
    vi.mocked(scriptsApi.create).mockReturnValue(
      new Promise(resolve => { resolveCreate = resolve }),
    )
    const user = userEvent.setup()
    renderWithClient(<ScriptFormModal onClose={vi.fn()} />)

    await user.type(getNameInput(), 'Mon script')
    await user.type(getContentTextarea(), 'echo hi')
    await user.click(getSubmitButton(/créer/i))

    expect(await screen.findByText(/enregistrement/i)).toBeInTheDocument()
    expect(getSubmitButton(/enregistrement/i)).toBeDisabled()

    resolveCreate({ data: SCRIPT })
  })

  it("affiche l'erreur retournée par l'API sans fermer la modale", async () => {
    vi.mocked(scriptsApi.create).mockRejectedValue(new Error('un script avec ce nom existe déjà'))
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScriptFormModal onClose={onClose} />)

    await user.type(getNameInput(), 'Doublon')
    await user.type(getContentTextarea(), 'echo hi')
    await user.click(getSubmitButton(/créer/i))

    expect(await screen.findByText(/un script avec ce nom existe déjà/i)).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('ScriptFormModal — édition', () => {
  it('pré-remplit les champs depuis le script existant', () => {
    renderWithClient(<ScriptFormModal script={SCRIPT} onClose={vi.fn()} />)

    expect(screen.getByText('Modifier le script')).toBeInTheDocument()
    expect(getNameInput()).toHaveValue('Existant')
    expect(getContentTextarea()).toHaveValue('print(1)')
  })

  it('met à jour le script via scriptsApi.update', async () => {
    vi.mocked(scriptsApi.update).mockResolvedValue({ data: SCRIPT })
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScriptFormModal script={SCRIPT} onClose={onClose} />)

    await user.clear(getContentTextarea())
    await user.type(getContentTextarea(), 'print(2)')
    await user.click(getSubmitButton(/enregistrer/i))

    await waitFor(() => expect(scriptsApi.update).toHaveBeenCalledWith('s1', {
      name: 'Existant', description: 'desc', interpreter: 'python', content: 'print(2)',
    }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })
})
