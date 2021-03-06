package telegrambot

import (
	"github.com/jinzhu/gorm"
	"github.com/yamnikov-oleg/avamon-bot/monitor"
)

type Record struct {
	ID     uint `gorm:"primary_key"`
	ChatID int64
	Title  string
	URL    string
}

func (r *Record) ToTarget() monitor.Target {
	return monitor.Target{
		ID:    r.ID,
		Title: r.Title,
		URL:   r.URL,
	}
}

// type TargetsGetter interface {
// 	GetTargets() ([]Target, error)
// }

type TargetsDB struct {
	DB *gorm.DB
}

func (t *TargetsDB) DeleteTarget(id int) error {
	err := t.DB.Where("ID = ?", id).Delete(Record{}).Error
	if err != nil {
		return err
	}
	return nil
}

func (t *TargetsDB) GetTarget(id int) (*Record, error) {
	r := Record{}
	err := t.DB.Where("ID = ?", id).First(&r).Error
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (t *TargetsDB) GetTargets() ([]monitor.Target, error) {
	records := []Record{}
	err := t.DB.Find(&records).Error
	if err != nil {
		return nil, err
	}
	var targets []monitor.Target
	for _, record := range records {
		targets = append(targets, record.ToTarget())
	}
	return targets, nil
}

func (t *TargetsDB) GetCurrentTargets(chatID int64) ([]Record, error) {
	records := []Record{}
	err := t.DB.Where("chat_id = ?", chatID).Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func (t *TargetsDB) CreateTarget(record Record) error {
	err := t.DB.Create(&record).Error
	if err != nil {
		return err
	}
	return nil
}

func (t *TargetsDB) Migrate() {
	t.DB.AutoMigrate(&Record{})
}
