package dataset

import (
	"context"
	"fmt"
	"strings"

	"github.com/ddy2006/packfs/internal/dataset"
	"github.com/ddy2006/packfs/internal/db"
	"github.com/kaichao/gopkg/errors"
	"github.com/spf13/cobra"
)

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List datasets",
		RunE: func(cmd *cobra.Command, args []string) error {
			sqlDB, err := db.OpenSQLite()
			if err != nil {
				return errors.WrapE(err, "open database")
			}
			defer sqlDB.Close()

			store := dataset.NewSQLiteStore(sqlDB)

			var filter dataset.Filter
			if id, _ := cmd.Flags().GetInt("dataset-id"); id > 0 {
				filter.ID = &id
			}
			if name, _ := cmd.Flags().GetString("dataset-name"); name != "" {
				filter.Name = &name
			}

			datasets, err := store.Find(context.Background(), filter)
			if err != nil {
				return errors.WrapE(err, "list datasets")
			}

			// 表格输出
			fmt.Println(strings.Join([]string{"ID", "Name", "CurrentPath", "Label"}, "\t"))
			for _, ds := range datasets {
				fmt.Printf("%d\t%s\t%s\t%s\n", ds.ID, ds.Name, ds.CurrentPath, ds.Label)
			}
			fmt.Printf("(%d rows)\n", len(datasets))
			return nil
		},
	}
	cmd.Flags().Int("dataset-id", 0, "filter by dataset ID")
	cmd.Flags().String("dataset-name", "", "filter by dataset name (substring match)")
	return cmd
}
