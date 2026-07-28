package arcset

import (
	"context"
	"fmt"
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
			id, _ := cmd.Flags().GetInt("id")

			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := arcset.NewSQLiteStore(sqlDB)

			// --id specified: list datasets in arcset
			if id > 0 {
				refs, err := store.ListDatasetRefs(context.Background(), id)
				if err != nil {
					return errors.WrapE(err, "list dataset refs")
				}
				a, err := store.FindByID(context.Background(), id)
				if err != nil {
					return errors.WrapE(err, "find arcset")
				}
				fmt.Printf("Datasets in arcset %s (id=%d, status=%s):\n", a.Name, a.ID, a.Status)
				if len(refs) == 0 {
					fmt.Println("  (none)")
				}
				for _, ref := range refs {
					fmt.Printf("  %d\t%s\n", ref.ID, ref.Name)
				}
				fmt.Printf("(%d datasets)\n", len(refs))
				return nil
			}

			// No --id: list all arcsets
			arcsets, err := store.Find(context.Background(), arcset.Filter{})
			if err != nil {
				return errors.WrapE(err, "list arcsets")
			}
			fmt.Println(strings.Join([]string{"ID", "Name", "Status", "CurrentPath"}, "\t"))
			for _, a := range arcsets {
				fmt.Printf("%d\t%s\t%s\t%s\n", a.ID, a.Name, a.Status, a.CurrentPath)
			}
			fmt.Printf("(%d arcsets)\n", len(arcsets))
			return nil
		},
	}
	cmd.Flags().Int("id", 0, "arcset ID (list its datasets)")
	return cmd
}
