# 🛡️ SentinelCore Agent

The lightweight, secure, and sandboxed telemetry and compliance agent for Linux systems, built for enterprise environments.

## ⚙️ Architecture & Security

* **Systemd Sandboxing:** Runs with extreme hardening (`ProtectSystem=strict`, `NoNewPrivileges=true`, `MemoryDenyWriteExecute`).
* **Low Footprint:** Written in Go with minimal resource consumption (`gopsutil`).
* **Auto-Enrollment:** Supports automated bootstrapping and registration with the SentinelCore Hub.

## 🚀 Installation via Ansible

Use the provided Ansible playbook to roll out the agent across hundreds of customer nodes instantly:

```bash
ansible-playbook -i inventory.ini install-agent.yml
