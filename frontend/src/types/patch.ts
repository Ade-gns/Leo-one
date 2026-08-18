export type PatchSeverity = 'optional' | 'important' | 'critical'
export type PatchStatus   = 'available' | 'installed' | 'ignored' | 'failed'

export interface Patch {
  id:           string
  tenant_id:    string
  agent_id:     string
  native_id:    string
  title:        string
  severity:     PatchSeverity
  size_bytes?:  number
  status:       PatchStatus
  detected_at:  string
  installed_at?: string
}

export interface InstallPatchesPayload {
  patch_ids:     string[]
  reboot_after?: boolean
}

export interface BulkInstallPatchesPayload {
  agent_ids?:    string[]
  workspace_id?: string
  min_severity?: PatchSeverity
  reboot_after?: boolean
}

export interface PatchesSummary {
  agents_with_critical_pending: number
  agents_with_pending_patches:  number
  total_pending_patches:        number
}
