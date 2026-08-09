package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	inventoryDomain "github.com/yourorg/leo-one/internal/domain/inventory"
)

// InventoryRepo implémente inventory.Repository via pgx/v5.
type InventoryRepo struct {
	pool *pgxpool.Pool
}

// NewInventoryRepo crée un InventoryRepo avec le pool de connexions fourni.
func NewInventoryRepo(pool *pgxpool.Pool) *InventoryRepo {
	return &InventoryRepo{pool: pool}
}

// strOrNil convertit une chaîne vide en NULL — l'agent C envoie "" pour un
// champ qu'il n'a pas pu déterminer (voir leo_hw_inventory_t), NULL est plus
// fidèle que "" pour ce cas en BDD.
func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// intOrNil convertit 0 en NULL — même convention côté agent (0 = indéterminable).
func intOrNil(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func int64OrNil(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

// SaveHardware enregistre un nouveau snapshot matériel (append-only).
func (r *InventoryRepo) SaveHardware(ctx context.Context, tenantID, agentID string, hw inventoryDomain.HardwareSnapshot) error {
	ctx = ensureCtx(ctx)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO hardware_inventory
			(agent_id, tenant_id, cpu_model, cpu_cores, cpu_threads,
			 ram_total_bytes, disk_count, bios_version, bios_vendor,
			 motherboard, serial_number, collected_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
	`,
		agentID, tenantID,
		strOrNil(hw.CPUModel), intOrNil(hw.CPUCores), intOrNil(hw.CPUThreads),
		int64OrNil(hw.RAMTotalBytes), intOrNil(hw.DiskCount),
		strOrNil(hw.BIOSVersion), strOrNil(hw.BIOSVendor),
		strOrNil(hw.Motherboard), strOrNil(hw.SerialNumber),
	)
	return err
}

// ReplaceSoftware remplace la liste des logiciels installés d'un agent.
// Delete + insert dans une transaction pour éviter qu'un lecteur ne voie une
// liste vide entre les deux opérations.
func (r *InventoryRepo) ReplaceSoftware(ctx context.Context, tenantID, agentID string, items []inventoryDomain.SoftwareSnapshotItem) error {
	ctx = ensureCtx(ctx)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op si déjà commit

	if _, err := tx.Exec(ctx, `
		DELETE FROM software_inventory WHERE tenant_id = $1 AND agent_id = $2
	`, tenantID, agentID); err != nil {
		return err
	}

	if len(items) > 0 {
		rows := make([][]any, len(items))
		for i, it := range items {
			rows[i] = []any{
				agentID, tenantID, it.Name,
				strOrNil(it.Version), strOrNil(it.Publisher), strOrNil(it.InstallPath),
			}
		}
		if _, err := tx.CopyFrom(ctx,
			pgx.Identifier{"software_inventory"},
			[]string{"agent_id", "tenant_id", "name", "version", "publisher", "install_path"},
			pgx.CopyFromRows(rows),
		); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// LatestHardware retourne le snapshot matériel le plus récent d'un agent.
func (r *InventoryRepo) LatestHardware(ctx context.Context, tenantID, agentID string) (*inventoryDomain.Hardware, error) {
	ctx = ensureCtx(ctx)

	var hw inventoryDomain.Hardware
	err := r.pool.QueryRow(ctx, `
		SELECT id, agent_id, cpu_model, cpu_cores, cpu_threads,
		       ram_total_bytes, disk_count, bios_version, bios_vendor,
		       motherboard, serial_number, collected_at
		FROM hardware_inventory
		WHERE tenant_id = $1 AND agent_id = $2
		ORDER BY collected_at DESC
		LIMIT 1
	`, tenantID, agentID).Scan(
		&hw.ID, &hw.AgentID, &hw.CPUModel, &hw.CPUCores, &hw.CPUThreads,
		&hw.RAMTotalBytes, &hw.DiskCount, &hw.BIOSVersion, &hw.BIOSVendor,
		&hw.Motherboard, &hw.SerialNumber, &hw.CollectedAt,
	)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &hw, nil
}

// ListSoftware retourne les logiciels installés d'un agent, triés par nom.
func (r *InventoryRepo) ListSoftware(ctx context.Context, tenantID, agentID, search string) ([]*inventoryDomain.SoftwareItem, error) {
	ctx = ensureCtx(ctx)

	query := `
		SELECT id, name, version, publisher, install_date, install_path, collected_at
		FROM software_inventory
		WHERE tenant_id = $1 AND agent_id = $2`
	args := []any{tenantID, agentID}

	if s := strings.TrimSpace(search); s != "" {
		query += ` AND name ILIKE $3`
		args = append(args, "%"+s+"%")
	}
	query += ` ORDER BY name ASC`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*inventoryDomain.SoftwareItem, 0)
	for rows.Next() {
		var it inventoryDomain.SoftwareItem
		if err := rows.Scan(
			&it.ID, &it.Name, &it.Version, &it.Publisher,
			&it.InstallDate, &it.InstallPath, &it.CollectedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &it)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	return items, nil
}

var _ inventoryDomain.Repository = (*InventoryRepo)(nil)
