# Run `vhs demo.tape` from rootdir to recreate demo
# Output file
# Output docs/demo.gif

# record a video demo
Output demo.mp4

# Terminal styling
# Set FontSize 16
# Set Width 1200
# Set Height 600
# Set Theme "GitHub Dark"

# optimal dimensions for better YT quality
Set Width 1920
Set Height 1080
Set FontSize 22
Set Theme "GitHub Dark"
Set FontFamily "Fira Code"
Set LetterSpacing 0
Set LineHeight 1.2
Set Framerate 30
Set Padding 60

# Typing speed configuration
Set TypingSpeed 35ms

# --- SETUP ---
Hide
Type "export PS1='\[\e[32m\]admin\$\[\e[0m\] '"
Enter
# Expect the kind cluster is already created following docs/TEST_README.md
Type "kubectl config use-context kind-nrr-test"
Enter
# Ensure the node starts in a "Tainted" state (Condition False -> Taint Present)
Type "./hack/test-workloads/flip-node-condition.sh nrr-test-worker network.k8s.io/CalicoReady false"
Enter
Type "clear"
Enter
Show

# --- DEMO START ---

# 1. Show the test setup
Hide
Type "# 1. Test setup: Kind cluster with a control-plane and a worker node"
Show
Sleep 1s
Enter
Type@15ms `kubectl get nodes`
Enter
Sleep 2s
Hide
Enter 2
Type "# NodeReadiness controller installed on control-plane node"
Show
Sleep 2s
Enter
Type "kubectl get deploy -n nrr-system"
Enter
Sleep 2s

# 2. Show the Worker node is currently Tainted
Hide
Enter 2
Type "# 2. Worker node has a Taint 'readiness.k8s.io/NetworkReady' because CalicoReady is missing/false"
Show
Sleep 2s
Enter
Type@15ms `kubectl get node nrr-test-worker -o custom-columns="NAME:.metadata.name,STATUS:.status.conditions[?(@.type=='network.k8s.io/CalicoReady')].status,TAINTS:.spec.taints[*].key"`
Enter 2
Sleep 3s

# 3. Show the Rule enforcing this
Hide
Type "clear"
Enter
Show
Enter 1
Type "# 2. Why? The NodeReadinessRule requires 'network.k8s.io/CalicoReady'"
Show
Sleep 2s
Enter
Type "cat examples/cni-readiness/network-readiness-rule.yaml"
Enter 1
Sleep 5s

# 4. Simulate the CNI becoming Ready
Hide
Enter 2
Type "# 4. Simulate the CNI plugin reporting 'Ready' status"
Show
Sleep 2s
Enter
Type@15ms "./hack/test-workloads/flip-node-condition.sh nrr-test-worker network.k8s.io/CalicoReady true"
Enter 1
Sleep 4s

# 5. Verify the Taint is gone
Hide
Enter 2
Type "# 5. NodeReadiness controller on observing the expected status change will remove the taint hold on the node" 
Show
Sleep 2s
Enter
Type@15ms `kubectl get node nrr-test-worker -o custom-columns="NAME:.metadata.name,STATUS:.status.conditions[?(@.type=='network.k8s.io/CalicoReady')].status,TAINTS:.spec.taints[*].key"`
Enter 2
Sleep 2s
Type "# Success! The Controller automatically removed the managed taint and node is ready for workloads"
Sleep 3s
