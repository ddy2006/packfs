package arcset

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ddy2006/packfs/internal/arcset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List arcsets or datasets in an arcset",
		RunE: func(cmd *cobra.Command, args []string) error {
			id, _ := cmd.Flags().GetInt64("id")
			status, _ := cmd.Flags().GetString("status")
			ctx := context.Background()
			pool, err := db.OpenPostgres(ctx)
			if err != nil {
				return errors.WrapE(err, "open postgres")
			}
			defer pool.Close()
			store := arcset.NewPGStore(pool)

			if id > 0 {
				a, err := store.FindByID(ctx, id)
				if err != nil {
					return errors.WrapE(err, "find arcset")
				}
				refs, err := store.ListRelations(ctx, id)
				if err != nil {
					return errors.WrapE(err, "list dataset refs")
				}
				fmt.Printf("Datasets in arcset %s (id=%d, status=%s/%s, health=%s):\n",
					a.Name, a.ID, a.Status, a.BuildPhase, a.Health)
				if len(refs) == 0 {
					fmt.Println("  (none)")
				}
				for _, ref := range refs {
					fmt.Printf("  order=%d key=%s uid=%s name=%s\n",
						ref.AppendOrder, ref.DatasetKey, ref.UID, ref.Name)
				}
				fmt.Printf("(%d datasets)\n", len(refs))
				return nil
			}

			arcsets, err := store.List(ctx, status)
			if err != nil {
				return errors.WrapE(err, "list arcsets")
			}
			fmt.Println(strings.Join([]string{"ID", "Name", "Status", "Phase", "Health", "Path"}, "\t"))
			for _, a := range arcsets {
				fmt.Fprintf(os.Stdout, "%d\t%s\t%s\t%s\t%s\t%s\n",
					a.ID, a.Name, a.Status, a.BuildPhase, a.Health, a.CurrentPath)
			}
			fmt.Printf("(%d arcsets)\n", len(arcsets))
			return nil
		},
	}
	cmd.Flags().Int64("id", 0, "arcset ID (list its datasets)")
	cmd.Flags().String("status", "", "filter by status")
	return cmd
}
