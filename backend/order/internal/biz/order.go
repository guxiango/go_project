package biz

import (
	"database/sql"

	"gorm.io/gorm"
)

// OrderStatus 订单状态
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"   // 待接单
	OrderStatusAccepted  OrderStatus = "accepted"  // 已接单
	OrderStatusStarted   OrderStatus = "started"   // 行程中
	OrderStatusFinished  OrderStatus = "finished"  // 已完成
	OrderStatusCancelled OrderStatus = "cancelled" // 已取消
)

// Order 订单表结构
type Order struct {
	gorm.Model
	OrderNo       string        `gorm:"type:varchar(64);uniqueIndex;not null"` // 订单编号
	CustomerID    uint          `gorm:"index;not null"`                        // 顾客id
	DriverID      sql.NullInt64 `gorm:"index"`                                 // 司机id
	Origin        string        `gorm:"type:varchar(255);not null"`            // 起点坐标
	Destination   string        `gorm:"type:varchar(255);not null"`            // 终点坐标
	Distance      int64         `gorm:"not null;default:0"`                    // 预计距离(米)
	Duration      int64         `gorm:"not null;default:0"`                    // 预计时长(秒)
	EstimatePrice int64         `gorm:"not null;default:0"`                    // 预估价格(分)

	Status string `gorm:"type:varchar(32);index;not null"` // 订单状态

	AcceptedAt  sql.NullTime // 司机接单时间
	StartedAt   sql.NullTime // 行程开始时间
	FinishedAt  sql.NullTime // 行程结束时间
	CancelledAt sql.NullTime // 订单取消时间
}

type OrderBiz struct {
}

func NewOrderBiz() *OrderBiz {
	return &OrderBiz{}
}
