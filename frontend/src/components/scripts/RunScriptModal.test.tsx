import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithClient } from '@/test/renderWithClient'
import { RunScriptModal } from './RunScriptModal'
import type { Script } from '@/types/script'
import type { Agent, BulkCommandResult } from '@/types/agent'
import type { Workspace } from '@/types/workspace'

vi.mock('@/api/agents', () => ({
  agentsApi: {
    list:           vi.fn(),
    bulkExecScript: vi.fn(),
  },
}))
vi.mock('@/api/workspaces', () => ({
  workspacesApi: { list: vi.fn() },
}))

import { agentsApi } from '@/api/agents'
import { workspacesApi } from '@/api/workspaces'

const SCRIPT: Script = {
  id: 's1', name: 'Nettoyage', interpreter: 'bash', content: 'echo hi',
  created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

const AGENTS: Agent[] = [
  { id: 'a1', tenant_id: 't1', hostname: 'PARIS-01', os: 'linux', os_version: '24.04', arch: 'amd64',
    hardware_id: 'hw1', agent_version: '1.0.0', status: 'online',
    enrolled_at: '2024-01-01T00:00:00Z', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
]

const WORKSPACES: Workspace[] = [
  { id: 'w1', tenant_id: 't1', name: 'Paris', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' },
]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(agentsApi.list).mockResolvedValue({ data: AGENTS })
  vi.mocked(workspacesApi.list).mockResolvedValue({ data: WORKSPACES })
})

describe('RunScriptModal', () => {
  it('désactive "Exécuter" tant qu\'aucune machine n\'est sélectionnée', async () => {
    renderWithClient(<RunScriptModal script={SCRIPT} onClose={vi.fn()} />)

    expect(await screen.findByText('PARIS-01')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /exécuter/i })).toBeDisabled()
  })

  it('exécute le script sur les machines cochées avec interpreter/content du script', async () => {
    const results: BulkCommandResult[] = [{ agent_id: 'a1', command_id: 'c1', sent: true }]
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: results })
    const user = userEvent.setup()
    renderWithClient(<RunScriptModal script={SCRIPT} onClose={vi.fn()} />)

    await user.click(await screen.findByRole('checkbox'))
    const run = screen.getByRole('button', { name: /exécuter/i })
    expect(run).toBeEnabled()
    await user.click(run)

    await waitFor(() => expect(agentsApi.bulkExecScript).toHaveBeenCalledWith({
      agent_ids: ['a1'], interpreter: 'bash', script: 'echo hi', timeout_sec: 60,
    }))
  })

  it('bascule sur le mode workspace et envoie workspace_id plutôt que agent_ids', async () => {
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: [] })
    const user = userEvent.setup()
    renderWithClient(<RunScriptModal script={SCRIPT} onClose={vi.fn()} />)

    await screen.findByText('PARIS-01')
    await user.click(screen.getByRole('button', { name: /workspace entier/i }))
    await user.selectOptions(screen.getByRole('combobox'), 'w1')
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    await waitFor(() => expect(agentsApi.bulkExecScript).toHaveBeenCalledWith({
      workspace_id: 'w1', interpreter: 'bash', script: 'echo hi', timeout_sec: 60,
    }))
  })

  it("affiche le résumé d'envoi après succès", async () => {
    const results: BulkCommandResult[] = [
      { agent_id: 'a1', command_id: 'c1', sent: true },
    ]
    vi.mocked(agentsApi.bulkExecScript).mockResolvedValue({ data: results })
    const user = userEvent.setup()
    renderWithClient(<RunScriptModal script={SCRIPT} onClose={vi.fn()} />)

    await user.click(await screen.findByRole('checkbox'))
    await user.click(screen.getByRole('button', { name: /exécuter/i }))

    expect(await screen.findByText(/envoyé à 1\/1 machine/i)).toBeInTheDocument()
  })
})
