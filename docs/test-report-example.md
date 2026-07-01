# Test Report Example

## Summary

- **Date**: 2026-07-01T10:30:00Z
- **Commit**: `1df5567`
- **Config**: `testdata/config-2chargers.yaml`
- **Scenario**: basic-2
- **Duration**: 120s
- **Result**: PASSED

## OCPP Summary

- BootNotification accepted for SIM-AC-001 and SIM-AC-002
- Heartbeat interval set to 60s
- RemoteStartTransaction triggered txID=1751334600
- SetChargingProfile limit=16A accepted
- MeterValues converged to ~16A within 2 sampling periods
- RemoteStopTransaction completed, txID=1751334600 stopped

## Meter Stats

- SIM-AC-001 max power: 3.63 kW
- SIM-AC-001 total energy: 0.121 kWh
- SIM-AC-002 max power: 6.27 kW
- SIM-AC-002 total energy: 0.209 kWh

## Failures

None.

## Web Console

- http://127.0.0.1:8088 accessible
- Both chargers visible in list
- Current curve updated in real-time
- Target current setting reflected in MeterValues within 1-2 sampling periods

## Conclusion

MVP smoke test passed. Ready for load-balancing integration test.
