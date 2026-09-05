package ir

// ParamType は catalog が定義するパラメータの型。
type ParamType string

const (
	ParamString ParamType = "string"
	ParamInt    ParamType = "int"
	ParamFloat  ParamType = "float"
	ParamBool   ParamType = "bool"
)

// ParamSpec は 1 つのパラメータの定義。catalog の params 定義から作られる。
type ParamSpec struct {
	Type ParamType
	// Required が true で、既定値も無いパラメータは省略できない。
	Required bool
	// HasDefault が true なら未指定でも catalog の既定値で補える。
	HasDefault bool
	// Enum が非空なら、値はこのいずれかでなければならない。
	Enum []string
}

// ServiceSpec は 1 サービス分のパラメータ定義。
type ServiceSpec struct {
	Params map[string]ParamSpec
}

// Schema は Resource.Service の値域と Resource.Params の型を提供する。
// 実装は catalog パッケージが担う（ADR-0005）。ir が catalog に依存しないよう
// インターフェースとして切っている。
type Schema interface {
	// ServiceNames は catalog が定義する全サービス名を返す（エラーメッセージ用）。
	ServiceNames() []string
	// ServiceSpec は指定サービスの定義を返す。無ければ ok が false。
	ServiceSpec(service string) (ServiceSpec, bool)
}
