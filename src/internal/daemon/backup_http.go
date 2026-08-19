package daemon

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/termux-nas/nas/internal/backup"
)

// --- 备份中心 API(用户通道,需登录) ---

// backupJobs GET /api/backup/jobs → 备份任务列表。
func (d *Daemon) backupJobs(c *fiber.Ctx) error {
	jobs, err := d.backups.Store().List()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"jobs": jobs})
}

// backupCreate POST /api/backup/jobs → 新建任务。
func (d *Daemon) backupCreate(c *fiber.Ctx) error {
	var body backup.Job
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "参数无效"})
	}
	if body.Name == "" || body.Source == "" || body.Target == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "name/source/target 必填"})
	}
	job, err := d.backups.Store().Create(body)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "job": job})
}

// backupUpdate PUT /api/backup/jobs/:id → 更新任务。
func (d *Daemon) backupUpdate(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID 无效"})
	}
	var body backup.Job
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "参数无效"})
	}
	body.ID = id
	if err := d.backups.Store().Update(body); err != nil {
		if err == backup.ErrJobNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// backupDelete DELETE /api/backup/jobs/:id → 删除任务。
func (d *Daemon) backupDelete(c *fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "ID 无效"})
	}
	if err := d.backups.Store().Delete(id); err != nil {
		if err == backup.ErrJobNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true})
}

// backupRun POST /api/backup/run → 立即执行任务 (body: {id})。
func (d *Daemon) backupRun(c *fiber.Ctx) error {
	var body struct {
		ID int64 `json:"id"`
	}
	if err := c.BodyParser(&body); err != nil || body.ID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少任务 ID"})
	}
	// 异步执行,立即返回(任务状态通过列表轮询查看)
	go d.backups.Run(body.ID, "manual")
	return c.JSON(fiber.Map{"ok": true, "id": body.ID, "status": "started"})
}

// backupRestore POST /api/backup/restore → 恢复(目标 → 源,方向反转)。
func (d *Daemon) backupRestore(c *fiber.Ctx) error {
	var body struct {
		ID int64 `json:"id"`
	}
	if err := c.BodyParser(&body); err != nil || body.ID <= 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "缺少任务 ID"})
	}
	job, err := d.backups.Store().Get(body.ID)
	if err != nil {
		if err == backup.ErrJobNotFound {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	// 恢复 = 反向执行:目标 → 源
	rev := backup.Job{Source: job.Target, Target: job.Source}
	go d.backups.RunJob(body.ID, "restore", &rev)
	return c.JSON(fiber.Map{"ok": true, "id": body.ID, "status": "restoring"})
}
