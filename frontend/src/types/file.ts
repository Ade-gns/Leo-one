export interface DeployableFile {
  id:              string
  tenant_id:       string
  name:            string
  size_bytes:      number
  checksum_sha256: string
  uploaded_by?:    string
  created_at:      string
}

export interface DeployFilePayload {
  file_id: string
}
