
## Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│ NodeReadiness   │    │ ReadinessGate    │    │ Validation      │
│ GateRule CRD    │────▶ Controller       │    │ Webhook         │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                │                       │
                                ▼                       │
                       ┌─────────────────┐              │
                       │ Node Taints     │              │
                       │ Management      │              │
                       └─────────────────┘              │
                                │                       │
                                ▼                       ▼
                       ┌─────────────────┐    ┌─────────────────┐
                       │ Kubernetes      │    │ Rule Conflict   │
                       │ Nodes           │    │ Detection       │
                       └─────────────────┘    └─────────────────┘

```

Detailed Flow:

```mermaid
graph TB
    %% CRD and Rules
    CRD[NodeReadinessRule CRD] --> RuleRec[RuleReconciler]

    %% Controller Components
    RuleRec --> Cache[Rule Cache]
    NodeRec[NodeReconciler] --> Cache
    Cache --> Controller[ReadinessGateController]

    %% Node Processing
    Nodes[Kubernetes Nodes] --> NodeRec
    NodeRec --> TaintMgmt[Taint Management]
    Controller --> TaintMgmt
    TaintMgmt --> Nodes

    %% Validation
    CRD --> Webhook[Validation Webhook]
    Webhook --> Validation[Conflict Detection<br/>& Rule Validation]

    %% External Systems
    NPD[Node Problem Detector<br/>& Health Checkers] --> Conditions[Node Conditions]
    Conditions --> Nodes

    %% Status and Observability
    Controller --> Status[Status Updates]
    Status --> CRD

    %% Enforcement Modes
    Controller --> Bootstrap{Bootstrap Mode?}
    Bootstrap -->|Yes| BootstrapLogic[One-time Taint Removal<br/>+ Annotation Marking]
    Bootstrap -->|No| ContinuousLogic[Continuous Monitoring<br/>+ Taint Management]

    %% Styling
    classDef crd fill:#e1f5fe
    classDef controller fill:#f3e5f5
    classDef external fill:#e8f5e8
    classDef validation fill:#fff3e0

    class CRD crd
    class RuleRec,NodeRec,Controller,Cache,TaintMgmt controller
    class NPD,Nodes,Conditions external
    class Webhook,Validation validation
```

### Core Components

#### 1. NodeReadinessRule CRD
- Defines rules mapping multiple node conditions to a single taint
- Supports bootstrap-only and continuous enforcement modes
- Allows node selector targeting and grace periods

#### 2. ReadinessGateController
- **RuleReconciler**: Processes rule changes and updates internal cache
- **NodeReconciler**: Handles node condition changes and evaluates applicable rules
- Manages taint addition/removal based on condition satisfaction

#### 3. [WIP] Validation Webhook
- Prevents conflicting rules (same taint key with overlapping node selectors)
- Validates rule specifications and required fields
- Ensures system consistency and prevents misconfigurations

#### 4. Integration with Node Problem Detector (NPD)
Works seamlessly with NPD or any system that sets node conditions:
- NPD plugins update node conditions (e.g., `example.io/CNIReady`)
- Controller watches condition changes and evaluates rules
- Supports custom conditions from any component