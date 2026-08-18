import { describe, it, expect, vi, beforeEach } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithClient } from '@/test/renderWithClient'
import { ScheduleFormModal } from './ScheduleFormModal'
import type { Script, ScriptSchedule } from '@/types/script'
import type { Agent } from '@/types/agent'
import type { Workspace } from '@/types/workspace'

vi.mock('@/api/scripts', () => ({
  scriptsApi:   { list: vi.fn() },
  schedulesApi: { list: vi.fn(), create: vi.fn(), update: vi.fn(), delete: vi.fn() },
}))
vi.mock('@/api/agents', () => ({
  agentsApi: { list: vi.fn() },
}))
vi.mock('@/api/workspaces', () => ({
  workspacesApi: { list: vi.fn() },
}))

import { scriptsApi, schedulesApi } from '@/api/scripts'
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

const SCHEDULE: ScriptSchedule = {
  id: 'sc1', script_id: 's1', name: 'Nocturne', agent_id: 'a1',
  cron_expression: '30 3 * * *', timeout_sec: 60, enabled: true,
  next_run_at: '2024-01-02T03:30:00Z', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z',
}

function getNameInput() {
  return screen.getByPlaceholderText('ex : Nettoyage nocturne')
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(scriptsApi.list).mockResolvedValue({ data: [SCRIPT] })
  vi.mocked(agentsApi.list).mockResolvedValue({ data: AGENTS })
  vi.mocked(workspacesApi.list).mockResolvedValue({ data: WORKSPACES })
})

describe('ScheduleFormModal — création', () => {
  it('désactive la soumission tant que nom/script/cible ne sont pas remplis', async () => {
    renderWithClient(<ScheduleFormModal onClose={vi.fn()} />)

    await screen.findByText('Nettoyage (bash)')
    expect(screen.getByRole('button', { name: /créer/i })).toBeDisabled()
  })

  it('crée une planification récurrente quotidienne (préréglage par défaut) sur un agent', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue({ data: SCHEDULE })
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal onClose={onClose} />)

    await user.type(getNameInput(), 'Nocturne')
    const [scriptSelect, agentSelect] = await screen.findAllByRole('combobox')
    await user.selectOptions(scriptSelect, 's1')
    await user.selectOptions(agentSelect, 'a1')

    const submit = screen.getByRole('button', { name: /créer/i })
    expect(submit).toBeEnabled()
    await user.click(submit)

    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalledWith({
      script_id: 's1', name: 'Nocturne', agent_id: 'a1', workspace_id: undefined,
      timeout_sec: 60, cron_expression: '0 2 * * *',
    }))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })

  it('cible un workspace entier plutôt qu\'un agent quand ce mode est choisi', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue({ data: SCHEDULE })
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal onClose={vi.fn()} />)

    await user.type(getNameInput(), 'Nocturne')
    const [scriptSelect] = await screen.findAllByRole('combobox')
    await user.selectOptions(scriptSelect, 's1')

    await user.click(screen.getByRole('button', { name: /workspace entier/i }))
    const selects = screen.getAllByRole('combobox')
    await user.selectOptions(selects[1], 'w1')

    await user.click(screen.getByRole('button', { name: /créer/i }))

    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalledWith(
      expect.objectContaining({ agent_id: undefined, workspace_id: 'w1' }),
    ))
  })

  it('planifie une exécution ponctuelle (run_at) quand "Une seule fois" est choisi', async () => {
    vi.mocked(schedulesApi.create).mockResolvedValue({ data: SCHEDULE })
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal onClose={vi.fn()} />)

    await user.type(getNameInput(), 'Ponctuelle')
    const [scriptSelect, agentSelect] = await screen.findAllByRole('combobox')
    await user.selectOptions(scriptSelect, 's1')
    await user.selectOptions(agentSelect, 'a1')

    // La valeur par défaut de run_at (dans ~5 min) est déjà dans le futur —
    // pas besoin de la modifier pour ce test.
    await user.click(screen.getByRole('button', { name: /une seule fois/i }))
    await user.click(screen.getByRole('button', { name: /créer/i }))

    await waitFor(() => expect(schedulesApi.create).toHaveBeenCalled())
    const payload = vi.mocked(schedulesApi.create).mock.calls[0][0]
    expect(payload.cron_expression).toBeUndefined()
    expect(payload.run_at).toBeDefined()
  })

  it("affiche l'erreur retournée par l'API sans fermer la modale", async () => {
    vi.mocked(schedulesApi.create).mockRejectedValue(new Error('script introuvable'))
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal onClose={onClose} />)

    await user.type(getNameInput(), 'Nocturne')
    const [scriptSelect, agentSelect] = await screen.findAllByRole('combobox')
    await user.selectOptions(scriptSelect, 's1')
    await user.selectOptions(agentSelect, 'a1')
    await user.click(screen.getByRole('button', { name: /créer/i }))

    expect(await screen.findByText(/script introuvable/i)).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('affiche un état de chargement pendant la création', async () => {
    let resolveCreate: (v: { data: ScriptSchedule }) => void = () => {}
    vi.mocked(schedulesApi.create).mockReturnValue(new Promise(resolve => { resolveCreate = resolve }))
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal onClose={vi.fn()} />)

    await user.type(getNameInput(), 'Nocturne')
    const [scriptSelect, agentSelect] = await screen.findAllByRole('combobox')
    await user.selectOptions(scriptSelect, 's1')
    await user.selectOptions(agentSelect, 'a1')
    await user.click(screen.getByRole('button', { name: /créer/i }))

    expect(await screen.findByText(/enregistrement/i)).toBeInTheDocument()
    resolveCreate({ data: SCHEDULE })
  })
})

describe('ScheduleFormModal — édition', () => {
  it('pré-remplit les champs depuis la planification existante', async () => {
    renderWithClient(<ScheduleFormModal schedule={SCHEDULE} onClose={vi.fn()} />)

    expect(screen.getByText('Modifier la planification')).toBeInTheDocument()
    expect(getNameInput()).toHaveValue('Nocturne')
    // cron "30 3 * * *" → préréglage "quotidien" à 03:30
    expect(await screen.findByDisplayValue('03:30')).toBeInTheDocument()
  })

  it('met à jour la planification via schedulesApi.update', async () => {
    vi.mocked(schedulesApi.update).mockResolvedValue({ data: SCHEDULE })
    const onClose = vi.fn()
    const user = userEvent.setup()
    renderWithClient(<ScheduleFormModal schedule={SCHEDULE} onClose={onClose} />)

    await user.clear(getNameInput())
    await user.type(getNameInput(), 'Nocturne v2')
    await user.click(screen.getByRole('button', { name: /enregistrer/i }))

    await waitFor(() => expect(schedulesApi.update).toHaveBeenCalledWith(
      'sc1', expect.objectContaining({ name: 'Nocturne v2' }),
    ))
    await waitFor(() => expect(onClose).toHaveBeenCalledTimes(1))
  })
})
