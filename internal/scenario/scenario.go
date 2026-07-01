package scenario

import (
	"fmt"
	"log"
	"time"

	"github.com/ljd166/ac-charger-simulator/internal/manager"
)

// Runner 场景运行器
type Runner struct {
	mgr *manager.Manager
}

// NewRunner 创建新运行器
func NewRunner(mgr *manager.Manager) *Runner {
	return &Runner{mgr: mgr}
}

// RunBasic2 基础 2 桩场景：连接所有桩，启动所有桩
func (r *Runner) RunBasic2() error {
	log.Println("[Scenario] basic-2: start")
	all := r.mgr.AllChargers()
	for _, c := range all {
		_ = c.Connect()
	}
	time.Sleep(2 * time.Second)
	for _, c := range all {
		_ = c.Start()
	}
	log.Println("[Scenario] basic-2: done")
	return nil
}

// RunRamp4 逐步启动 4 桩
func (r *Runner) RunRamp4() error {
	log.Println("[Scenario] ramp-4: start")
	all := r.mgr.AllChargers()
	for i, c := range all {
		_ = c.Connect()
		time.Sleep(1 * time.Second)
		_ = c.Start()
		if i < 3 {
			time.Sleep(5 * time.Second)
		}
	}
	log.Println("[Scenario] ramp-4: done")
	return nil
}

// RunOvernight8Short 8 桩短压测
func (r *Runner) RunOvernight8Short() error {
	log.Println("[Scenario] overnight-8-short: start")
	all := r.mgr.AllChargers()
	for _, c := range all {
		_ = c.Connect()
	}
	time.Sleep(2 * time.Second)
	for _, c := range all {
		_ = c.Start()
	}
	log.Println("[Scenario] overnight-8-short: running 30 min...")
	time.Sleep(30 * time.Minute)
	for _, c := range all {
		_ = c.Stop()
	}
	log.Println("[Scenario] overnight-8-short: done")
	return nil
}

// RunPhaseL1Overload L1 过载场景
func (r *Runner) RunPhaseL1Overload() error {
	log.Println("[Scenario] phase-l1-overload: start")
	all := r.mgr.AllChargers()
	for _, c := range all {
		if c.Config().PhaseAssignment == "L1" {
			_ = c.Connect()
			_ = c.Start()
		}
	}
	log.Println("[Scenario] phase-l1-overload: done")
	return nil
}

// RunFaultAndRecover 故障与恢复
func (r *Runner) RunFaultAndRecover() error {
	log.Println("[Scenario] fault-and-recover: start")
	all := r.mgr.AllChargers()
	if len(all) == 0 {
		return fmt.Errorf("no chargers")
	}
	c := all[0]
	_ = c.Connect()
	_ = c.Start()
	time.Sleep(5 * time.Second)
	c.SetFault("EarthFailure")
	time.Sleep(5 * time.Second)
	c.ClearFault()
	_ = c.Start()
	log.Println("[Scenario] fault-and-recover: done")
	return nil
}
