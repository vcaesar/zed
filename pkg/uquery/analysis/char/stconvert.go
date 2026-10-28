package char

import (
	"github.com/vcaesar/riot/analysis"
	zedchar "github.com/vcaesar/zedsearch/pkg/bluge/analysis/char"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

func NewSTConvertCharFilter(options interface{}) (analysis.CharFilter, error) {
	conversion, err := zutils.GetStringFromMap(options, "convert_type")
	if err != nil {
		return nil, err
	}
	return zedchar.NewSTConvertCharFilter(conversion)
}
