package data

import (
	"context"
	"valuation/internal/biz"
)

type PriceRuleData struct {
	data *Data
}

func NewPriceRuleInterface(data *Data) biz.PriceRuleInterface {
	return &PriceRuleData{data: data}
}

// 获取价格规则
func (d *PriceRuleData) GetRule(ctx context.Context, cityId uint, curr int) (*biz.PriceRule, error) {
	priceRule := &biz.PriceRule{}
	result := d.data.Mdb.Where("city_id = ? AND start_time <= ? AND end_time > ?", cityId, curr, curr).First(priceRule)
	if result.Error != nil {
		return nil, result.Error
	}
	return priceRule, nil
}