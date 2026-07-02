package main

// DatasetDef 对应 dataset.def 文件的完整配置
type DatasetDef struct {
	Dataset    DatasetConfig    `yaml:"dataset"`
	Shard      ShardConfig      `yaml:"shard"`
	Simulation SimulationConfig `yaml:"simulation"`
}

// DatasetConfig 数据集配置
type DatasetConfig struct {
	Name    string `yaml:"name"`
	StartTS int64  `yaml:"start_ts"`
	EndTS   int64  `yaml:"end_ts"`
	ChStart int    `yaml:"ch_start"`
	ChEnd   int    `yaml:"ch_end"`
}

// ShardConfig shard 打包配置
type ShardConfig struct {
	GroupBy   string `yaml:"group_by"`
	GroupSize int    `yaml:"group_size"`
	Compress  string `yaml:"compress"`
	Format    string `yaml:"format"`
}

// SimulationConfig 模拟数据配置
type SimulationConfig struct {
	FileBytes int `yaml:"file_bytes"`
}

// defConfig 全局配置，在 init() 中加载
var defConfig *DatasetDef
