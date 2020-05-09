package conf

import (
    "github.com/Multi-Tier-Cloud/common/p2putil"
)

type Config struct {
    Perf       struct {
        SoftReq p2putil.PerfInd
        HardReq p2putil.PerfInd
    }
    Bootstraps []string
}
