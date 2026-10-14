package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gliese129/runq-lab/internal/consumer"
)

func (s *Store) InsertConsumer(ctx context.Context, c *consumer.Consumer) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO consumers(id,job_id,name,target,generation,status,data_json) VALUES(?,?,?,?,?,?,?)`, c.ID, c.JobID, c.Name, c.Target, c.Generation, c.Status, string(raw))
	return err
}
func (s *Store) UpdateConsumer(ctx context.Context, c *consumer.Consumer) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE consumers SET status=?,data_json=?,final_ingest=? WHERE id=?`, c.Status, string(raw), boolToInt(c.FinalIngest), c.ID)
	return err
}
func (s *Store) ListConsumers(ctx context.Context, jobID, target string) ([]consumer.Consumer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data_json,final_ingest FROM consumers WHERE (?='' OR job_id=?) AND (?='' OR target=?) ORDER BY id`, jobID, jobID, target, target)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []consumer.Consumer{}
	for rows.Next() {
		var c consumer.Consumer
		var raw string
		var final int
		if err := rows.Scan(&raw, &final); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		c.FinalIngest = final != 0
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) GetConsumer(ctx context.Context, id string) (*consumer.Consumer, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT data_json,final_ingest FROM consumers WHERE substr(id,1,length(?))=? LIMIT 2`, id, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out *consumer.Consumer
	for rows.Next() {
		if out != nil {
			return nil, fmt.Errorf("ambiguous consumer prefix %q", id)
		}
		var c consumer.Consumer
		var raw string
		var final int
		if err := rows.Scan(&raw, &final); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return nil, err
		}
		c.FinalIngest = final != 0
		out = &c
	}
	return out, rows.Err()
}
func (s *Store) ConsumerFileMark(ctx context.Context, id, file string) (FileIngestMark, error) {
	m := FileIngestMark{}
	rows, err := s.db.QueryContext(ctx, `SELECT file_size,parsed_offset,final FROM source_file_ingest WHERE source_id=? AND file=?`, id, file)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	if rows.Next() {
		var final int
		err = rows.Scan(&m.Size, &m.Offset, &final)
		m.Final = final != 0
	}
	return m, err
}

type ConsumerMetric struct {
	Key   string
	Value float64
	TS    int64
	Step  *float64
}

func (s *Store) ApplyConsumerIngest(ctx context.Context, c consumer.Consumer, file string, rebuild bool, records []ResultRecordRow, metrics []ConsumerMetric, mark FileIngestMark) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if rebuild {
		table := "record_latest"
		field := "task_id"
		if file == "metrics" {
			table = "consumer_metrics"
			field = "source_id"
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+field+`=?`, c.ID); err != nil {
			return err
		}
	}
	for _, r := range records {
		if err = upsertLatest(ctx, tx, r); err != nil {
			return err
		}
	}
	for _, m := range metrics {
		if _, err = tx.ExecContext(ctx, `INSERT INTO consumer_metrics(source_id,job_id,key,last,last_ts,step) VALUES(?,?,?,?,?,?) ON CONFLICT(source_id,key) DO UPDATE SET last=excluded.last,last_ts=excluded.last_ts,step=excluded.step WHERE excluded.last_ts>=consumer_metrics.last_ts`, c.ID, c.JobID, m.Key, m.Value, m.TS, m.Step); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO source_file_ingest(source_id,file,file_size,parsed_offset,final) VALUES(?,?,?,?,?) ON CONFLICT(source_id,file) DO UPDATE SET file_size=excluded.file_size,parsed_offset=excluded.parsed_offset,final=excluded.final`, c.ID, file, mark.Size, mark.Offset, boolToInt(mark.Final))
	if err != nil {
		return err
	}
	return tx.Commit()
}
