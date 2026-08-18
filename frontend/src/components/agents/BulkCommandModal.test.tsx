import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithClient } from '@/test/renderWithClient'
import { BulkCommandModal } from './BulkCommandModal'
import type { BulkCommandResult } from '@/types/agent'
import type { Workspace } from '@/types/workspace'

vi.mock('@/api/agents', () => ({
  agentsApi: { bulkExecScript: vi.fn() },
}))
vi.mock('@/api/workspaces', () => ({
  workspacesApi: { list: vi.fn() },
}))

import { agentsApi } from '@/api/agents'
import { workspacesApi } from '@/api/workspaces'

const WORKSPACES: Workspace[] = [
  { id: 'w1', tenant_id: 't1', name: 'Paris', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
]

function getScriptTextarea() {
  return document.querySelector('textarea') as HTMLTextAreaElement
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(workspacesApi.list).mockResolvedValue({ data: WORKSPACES })
})

describe('BulkCommandModal — machines présélectionnées', () => {
  it('désactive "Exécuter" tant que le script est vide', () => {
    renderWithClient(<BulkCommandModal selectedAgentIds={['a1', 'a2']} onClose={vi.fn()} />)

    expect(screen.getByRole('button', { name: /exécuter/i })).toBeDisabled()
  })

  it('envoie agent_ids + interpreter + script par défaut (bash)', async () => {
    const results: BulkCommandResult[] = [
      { agent_id: 'a1', command_id: 'c1', sent: true },
      { agent_id: 'a2', command_id: 'c2', sent: true },
    ]
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: results })
    const user = userEvent.setup()
    renderWithClient(<BulkCommandModal selectedAgentIds={['a1', 'a2']} onClose={vi.fn()} />)

    await user.type(getScriptTextarea(), 'echo hi')
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    await waitFor(() => expect(agentsApi.bulkExecScript).toHaveBeenCalledWith({
      agent_ids: ['a1', 'a2'], interpreter: 'bash', script: 'echo hi', timeout_sec: 60,
    }))
    expect(await screen.findByText(/envoyé à 2\/2 machines/i)).toBeInTheDocument()
  })

  it("change l'interpréteur envoyé quand un autre shell est sélectionné", async () => {
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: [] })
    const user = userEvent.setup()
    renderWithClient(<BulkCommandModal selectedAgentIds={['a1']} onClose={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: 'PowerShell' }))
    await user.type(getScriptTextarea(), 'Write-Output hi')
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    await waitFor(() => expect(agentsApi.bulkExecScript).toHaveBeenCalledWith({
      agent_ids: ['a1'], interpreter: 'powershell', script: 'Write-Output hi', timeout_sec: 60,
    }))
  })
})

describe('BulkCommandModal — sans présélection', () => {
  it('démarre en mode workspace et exige un workspace choisi', async () => {
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: [] })
    const user = userEvent.setup()
    renderWithClient(<BulkCommandModal selectedAgentIds={[]} onClose={vi.fn()} />)

    await user.type(getScriptTextarea(), 'echo hi')
    expect(screen.getByRole('button', { name: /exécuter/i })).toBeDisabled()

    await user.selectOptions(await screen.findByRole('combobox'), 'w1')
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    await waitFor(() => expect(agentsApi.bulkExecScript).toHaveBeenCalledWith({
      workspace_id: 'w1', interpreter: 'bash', script: 'echo hi', timeout_sec: 60,
    }))
  })

  it('le mode "machines sélectionnées" est désactivé sans présélection', () => {
    renderWithClient(<BulkCommandModal selectedAgentIds={[]} onClose={vi.fn()} />)

    expect(screen.getByRole('button', { name: /0 machine.* sélectionnée/i })).toBeDisabled()
  })
})

describe('BulkCommandModal — erreur', () => {
  it("affiche l'erreur de l'API dans le résumé plutôt que de planter", async () => {
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({
      data: [{ agent_id: 'a1', sent: false, error: 'agent hors ligne' }],
    })
    const user = userEvent.setup()
    renderWithClient(<BulkCommandModal selectedAgentIds={['a1']} onClose={vi.fn()} />)

    await user.type(getScriptTextarea(), 'echo hi')
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    expect(await screen.findByText(/erreur : agent hors ligne/i)).toBeInTheDocument()
  })
})
