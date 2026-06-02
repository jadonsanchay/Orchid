package main

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Orchid Orchestrator Dashboard</title>
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
    <link href="https://fonts.googleapis.com/css2?family=Outfit:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0b0f19;
            --surface-color: rgba(17, 24, 39, 0.7);
            --surface-border: rgba(255, 255, 255, 0.08);
            --primary-gradient: linear-gradient(135deg, #a855f7, #ec4899);
            --primary-color: #a855f7;
            --success-color: #10b981;
            --success-bg: rgba(16, 185, 129, 0.1);
            --error-color: #ef4444;
            --error-bg: rgba(239, 68, 68, 0.1);
            --pending-color: #64748b;
            --running-color: #3b82f6;
            --text-main: #f3f4f6;
            --text-muted: #9ca3af;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            font-family: 'Outfit', sans-serif;
            background-color: var(--bg-color);
            color: var(--text-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
            overflow-x: hidden;
            background-image: 
                radial-gradient(circle at 10% 20%, rgba(168, 85, 247, 0.15) 0%, transparent 40%),
                radial-gradient(circle at 90% 80%, rgba(236, 72, 153, 0.1) 0%, transparent 40%);
        }

        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 1.5rem 2rem;
            border-bottom: 1px solid var(--surface-border);
            backdrop-filter: blur(12px);
            background-color: rgba(11, 15, 25, 0.8);
            position: sticky;
            top: 0;
            z-index: 100;
        }

        .logo-container {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .logo-icon {
            width: 2rem;
            height: 2rem;
            background: var(--primary-gradient);
            border-radius: 0.5rem;
            display: flex;
            align-items: center;
            justify-content: center;
            font-weight: 700;
            color: white;
            font-size: 1.25rem;
            box-shadow: 0 0 15px rgba(168, 85, 247, 0.4);
        }

        .logo-title {
            font-size: 1.5rem;
            font-weight: 700;
            letter-spacing: -0.025em;
            background: var(--primary-gradient);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .btn {
            background: var(--primary-gradient);
            border: none;
            color: white;
            padding: 0.75rem 1.5rem;
            border-radius: 0.5rem;
            font-weight: 600;
            cursor: pointer;
            transition: all 0.2s ease;
            box-shadow: 0 4px 15px rgba(168, 85, 247, 0.25);
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .btn:hover {
            transform: translateY(-2px);
            box-shadow: 0 6px 20px rgba(168, 85, 247, 0.4);
        }

        .btn:active {
            transform: translateY(0);
        }

        .container {
            flex: 1;
            display: grid;
            grid-template-columns: 350px 1fr;
            height: calc(100vh - 73px);
        }

        .sidebar {
            border-right: 1px solid var(--surface-border);
            padding: 1.5rem;
            overflow-y: auto;
            background-color: rgba(11, 15, 25, 0.4);
            display: flex;
            flex-direction: column;
            gap: 1rem;
        }

        .sidebar-header {
            font-size: 1.1rem;
            font-weight: 600;
            color: var(--text-muted);
            letter-spacing: 0.05em;
            text-transform: uppercase;
        }

        .run-list {
            display: flex;
            flex-direction: column;
            gap: 0.75rem;
        }

        .run-item {
            background: var(--surface-color);
            border: 1px solid var(--surface-border);
            border-radius: 0.75rem;
            padding: 1rem;
            cursor: pointer;
            transition: all 0.25s ease;
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .run-item:hover {
            border-color: rgba(168, 85, 247, 0.4);
            transform: translateX(3px);
            background: rgba(17, 24, 39, 0.9);
        }

        .run-item.active {
            border-color: var(--primary-color);
            background: rgba(168, 85, 247, 0.06);
            box-shadow: inset 0 0 10px rgba(168, 85, 247, 0.05);
        }

        .run-meta {
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .run-id {
            font-weight: 600;
            font-family: 'JetBrains Mono', monospace;
            color: var(--text-main);
        }

        .badge {
            font-size: 0.75rem;
            padding: 0.25rem 0.6rem;
            border-radius: 2rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }

        .badge-pending { color: var(--pending-color); background: rgba(100, 116, 139, 0.1); }
        .badge-running { color: var(--running-color); background: rgba(59, 130, 246, 0.1); animation: pulse-blue 1.5s infinite; }
        .badge-completed { color: var(--success-color); background: var(--success-bg); }
        .badge-failed { color: var(--error-color); background: var(--error-bg); }

        .run-time {
            font-size: 0.8rem;
            color: var(--text-muted);
        }

        .main-content {
            padding: 2rem;
            overflow-y: auto;
            display: flex;
            flex-direction: column;
            gap: 2rem;
        }

        .run-detail-header {
            background: var(--surface-color);
            border: 1px solid var(--surface-border);
            border-radius: 1rem;
            padding: 1.5rem 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            backdrop-filter: blur(10px);
        }

        .detail-title {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .detail-info {
            display: flex;
            flex-direction: column;
            gap: 0.25rem;
        }

        .detail-user {
            font-size: 0.9rem;
            color: var(--text-muted);
        }

        .flow-container {
            display: grid;
            grid-template-columns: repeat(4, 1fr);
            gap: 1.5rem;
            position: relative;
        }

        .flow-card {
            background: var(--surface-color);
            border: 1px solid var(--surface-border);
            border-radius: 1rem;
            padding: 1.5rem;
            display: flex;
            flex-direction: column;
            gap: 1rem;
            transition: all 0.3s ease;
            position: relative;
            backdrop-filter: blur(10px);
        }

        .flow-card.active-step {
            border-color: var(--running-color);
            box-shadow: 0 0 20px rgba(59, 130, 246, 0.15);
        }

        .flow-card.completed-step {
            border-color: var(--success-color);
        }

        .flow-card.failed-step {
            border-color: var(--error-color);
        }

        .step-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
        }

        .step-number {
            font-size: 0.8rem;
            color: var(--text-muted);
            font-weight: 600;
        }

        .step-name {
            font-size: 1.2rem;
            font-weight: 600;
            text-transform: capitalize;
        }

        .step-attempts {
            font-size: 0.8rem;
            color: var(--text-muted);
        }

        .step-body {
            font-size: 0.9rem;
            color: var(--text-muted);
            min-height: 50px;
        }

        .step-status-icon {
            width: 1.5rem;
            height: 1.5rem;
            border-radius: 50%;
            display: flex;
            align-items: center;
            justify-content: center;
            font-weight: 700;
        }

        .icon-pending { background: rgba(100, 116, 139, 0.15); color: var(--pending-color); }
        .icon-running { background: rgba(59, 130, 246, 0.2); color: var(--running-color); animation: spin 2s linear infinite; }
        .icon-completed { background: rgba(16, 185, 129, 0.2); color: var(--success-color); }
        .icon-failed { background: rgba(239, 68, 68, 0.2); color: var(--error-color); }

        .output-card {
            background: var(--surface-color);
            border: 1px solid var(--surface-border);
            border-radius: 1rem;
            padding: 1.5rem;
            display: flex;
            flex-direction: column;
            gap: 1rem;
            backdrop-filter: blur(10px);
        }

        .output-title {
            font-size: 1.1rem;
            font-weight: 600;
            color: var(--text-main);
            border-bottom: 1px solid var(--surface-border);
            padding-bottom: 0.5rem;
        }

        pre {
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.85rem;
            padding: 1rem;
            background: rgba(0, 0, 0, 0.3);
            border-radius: 0.5rem;
            overflow-x: auto;
            color: #38bdf8;
            max-height: 350px;
        }

        .modal-overlay {
            position: fixed;
            top: 0;
            left: 0;
            width: 100vw;
            height: 100vh;
            background: rgba(0, 0, 0, 0.7);
            backdrop-filter: blur(5px);
            display: flex;
            align-items: center;
            justify-content: center;
            z-index: 1000;
            opacity: 0;
            pointer-events: none;
            transition: opacity 0.3s ease;
        }

        .modal-overlay.open {
            opacity: 1;
            pointer-events: auto;
        }

        .modal {
            background: #0f172a;
            border: 1px solid var(--surface-border);
            border-radius: 1.25rem;
            width: 600px;
            max-width: 90%;
            padding: 2rem;
            box-shadow: 0 10px 30px rgba(0, 0, 0, 0.5);
            display: flex;
            flex-direction: column;
            gap: 1.5rem;
            transform: scale(0.9);
            transition: transform 0.3s cubic-bezier(0.34, 1.56, 0.64, 1);
        }

        .modal-overlay.open .modal {
            transform: scale(1);
        }

        .modal-header {
            font-size: 1.3rem;
            font-weight: 700;
            color: var(--text-main);
        }

        .form-group {
            display: flex;
            flex-direction: column;
            gap: 0.5rem;
        }

        .form-group label {
            font-size: 0.9rem;
            font-weight: 500;
            color: var(--text-muted);
        }

        .form-control {
            background: rgba(255, 255, 255, 0.05);
            border: 1px solid var(--surface-border);
            border-radius: 0.5rem;
            padding: 0.75rem 1rem;
            color: var(--text-main);
            font-family: inherit;
            outline: none;
            transition: border-color 0.2s;
        }

        .form-control:focus {
            border-color: var(--primary-color);
        }

        .modal-actions {
            display: flex;
            justify-content: flex-end;
            gap: 1rem;
            margin-top: 1rem;
        }

        .btn-secondary {
            background: transparent;
            border: 1px solid var(--surface-border);
            color: var(--text-main);
            box-shadow: none;
        }

        .btn-secondary:hover {
            background: rgba(255, 255, 255, 0.05);
            box-shadow: none;
            transform: translateY(0);
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            gap: 1rem;
            color: var(--text-muted);
            height: 300px;
        }

        @keyframes pulse-blue {
            0% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); }
            70% { box-shadow: 0 0 0 8px rgba(59, 130, 246, 0); }
            100% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0); }
        }

        @keyframes spin {
            0% { transform: rotate(0deg); }
            100% { transform: rotate(360deg); }
        }
    </style>
</head>
<body>
    <header>
        <div class="logo-container">
            <div class="logo-icon">O</div>
            <div class="logo-title">Orchid Orchestrator</div>
        </div>
        <button class="btn" onclick="openModal()">
            <svg width="16" height="16" fill="currentColor" viewBox="0 0 16 16">
                <path d="M8 4a.5.5 0 0 1 .5.5v3h3a.5.5 0 0 1 0 1h-3v3a.5.5 0 0 1-1 0v-3h-3a.5.5 0 0 1 0-1h3v-3A.5.5 0 0 1 8 4z"/>
            </svg>
            New Workflow Run
        </button>
    </header>

    <div class="container">
        <div class="sidebar">
            <div class="sidebar-header">Recent runs</div>
            <div class="run-list" id="run-list">
                <!-- Runs populated dynamically -->
            </div>
        </div>
        <div class="main-content" id="main-content">
            <div class="empty-state">
                <h3>Select a run to view execution details</h3>
                <p>Or click 'New Workflow Run' to start a new job search pipeline.</p>
            </div>
        </div>
    </div>

    <!-- Modal for new run -->
    <div class="modal-overlay" id="modal-overlay">
        <div class="modal">
            <div class="modal-header">Trigger Job Search Workflow</div>
            <div class="form-group">
                <label for="user-id">User Identifier</label>
                <input type="text" id="user-id" class="form-control" value="user-999">
            </div>
            <div class="form-group">
                <label for="resume-text">Resume Profile Text</label>
                <textarea id="resume-text" class="form-control" rows="8" style="resize: none;">Experienced software engineer specializing in backend systems. Strong proficiency in Golang, microservices, and databases like PostgreSQL. Experience deploying message broker queues like Redis. Looking to build workflow orchestrator engines.</textarea>
            </div>
            <div class="modal-actions">
                <button class="btn btn-secondary" onclick="closeModal()">Cancel</button>
                <button class="btn" onclick="submitRun()">Trigger Run</button>
            </div>
        </div>
    </div>

    <script>
        let activeRunID = null;
        let pollInterval = null;

        document.addEventListener('DOMContentLoaded', () => {
            fetchRuns();
            setInterval(fetchRuns, 3000);
        });

        function fetchRuns() {
            fetch('/api/runs')
                .then(res => res.json())
                .then(runs => {
                    const runList = document.getElementById('run-list');
                    if (runs.length === 0) {
                        runList.innerHTML = '<div style="color:var(--text-muted);text-align:center;padding:2rem;">No runs found</div>';
                        return;
                    }

                    runList.innerHTML = runs.map(run => {
                        const date = new Date(run.created_at).toLocaleString();
                        const isActive = run.id === activeRunID ? 'active' : '';
                        return '<div class="run-item ' + isActive + '" onclick="selectRun(' + run.id + ')">' +
                                '<div class="run-meta">' +
                                    '<span class="run-id">Run #' + run.id + '</span>' +
                                    '<span class="badge badge-' + run.status + '">' + run.status + '</span>' +
                                '</div>' +
                                '<div class="run-time">' + date + '</div>' +
                            '</div>';
                    }).join('');
                })
                .catch(err => console.error("Error fetching runs:", err));
        }

        function selectRun(id) {
            activeRunID = id;
            fetchRuns(); // update active class immediately
            fetchRunDetail(id);

            if (pollInterval) clearInterval(pollInterval);
            pollInterval = setInterval(() => {
                if (activeRunID === id) fetchRunDetail(id);
            }, 1000);
        }

        function fetchRunDetail(id) {
            fetch('/api/runs?id=' + id)
                .then(res => res.json())
                .then(data => {
                    const mainContent = document.getElementById('main-content');
                    const run = data.run;
                    const steps = data.steps || [];

                    const stepsMap = {};
                    steps.forEach(step => {
                        stepsMap[step.task_name] = step;
                    });

                    const pipeline = ['ingest', 'match', 'tailor', 'apply'];

                    const flowCardsHtml = pipeline.map((name, index) => {
                        const stepData = stepsMap[name];
                        let status = 'pending';
                        let attempt = 0;
                        let lastErr = '';

                        if (stepData) {
                            status = stepData.status;
                            attempt = stepData.attempt_count;
                            if (stepData.last_error) lastErr = stepData.last_error;
                        }

                        let statusIcon = '●';
                        if (status === 'running') statusIcon = '↻';
                        if (status === 'completed') statusIcon = '✓';
                        if (status === 'failed') statusIcon = '✕';

                        let statusClass = 'flow-card';
                        if (status === 'running') statusClass += ' active-step';
                        if (status === 'completed') statusClass += ' completed-step';
                        if (status === 'failed') statusClass += ' failed-step';

                        let stepDesc = '';
                        if (name === 'ingest') stepDesc = 'Seed jobs list & compute vector embeddings.';
                        if (name === 'match') stepDesc = 'Compute resume vector & query pgvector similarities.';
                        if (name === 'tailor') stepDesc = 'Prepend optimized executive summary for top match.';
                        if (name === 'apply') stepDesc = 'Simulate portal application and record transaction.';

                        return '<div class="' + statusClass + '">' +
                                '<div class="step-header">' +
                                    '<span class="step-number">STEP 0' + (index + 1) + '</span>' +
                                    '<div class="step-status-icon icon-' + status + '">' + statusIcon + '</div>' +
                                '</div>' +
                                '<div class="step-name">' + name + '</div>' +
                                '<div class="step-body">' + stepDesc + '</div>' +
                                (attempt > 0 ? '<div class="step-attempts">Attempt ' + attempt + '</div>' : '') +
                                (lastErr ? '<div style="color:var(--error-color);font-size:0.8rem;margin-top:0.5rem;word-break:break-all;">Error: ' + lastErr + '</div>' : '') +
                            '</div>';
                    }).join('');

                    let activeOutputsHtml = '';
                    pipeline.forEach(name => {
                        const stepData = stepsMap[name];
                        if (stepData && stepData.output) {
                            activeOutputsHtml += '<div class="output-card">' +
                                    '<div class="output-title">Step: ' + name + ' (Output Checkpoint)</div>' +
                                    '<pre><code>' + JSON.stringify(stepData.output, null, 2) + '</code></pre>' +
                                '</div>';
                        }
                    });

                    const date = new Date(run.created_at).toLocaleString();

                    mainContent.innerHTML = '<div class="run-detail-header">' +
                            '<div class="detail-info">' +
                                '<h2 style="font-weight:700;">Workflow Run #' + run.id + '</h2>' +
                                '<span class="detail-user">User: ' + run.user_id + ' · Started: ' + date + '</span>' +
                            '</div>' +
                            '<span class="badge badge-' + run.status + '">' + run.status + '</span>' +
                        '</div>' +
                        '<h3 style="font-weight:600;margin-top:1.5rem;color:var(--text-muted)">EXECUTION STEPS</h3>' +
                        '<div class="flow-container">' +
                            flowCardsHtml +
                        '</div>' +
                        (activeOutputsHtml ? 
                            '<h3 style="font-weight:600;margin-top:2rem;color:var(--text-muted)">LOGS & OUTPUT CHECKPOINTS</h3>' +
                            '<div style="display:flex;flex-direction:column;gap:1.5rem;margin-top:0.5rem;">' +
                                activeOutputsHtml +
                            '</div>' : '');
                })
                .catch(err => console.error("Error fetching run details:", err));
        }

        function openModal() {
            document.getElementById('modal-overlay').classList.add('open');
        }

        function closeModal() {
            document.getElementById('modal-overlay').classList.remove('open');
        }

        function submitRun() {
            const userId = document.getElementById('user-id').value;
            const resumeText = document.getElementById('resume-text').value;

            if (!userId || !resumeText) {
                alert("Please fill in both fields");
                return;
            }

            fetch('/api/runs', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ user_id: userId, resume_text: resumeText })
            })
            .then(res => res.json())
            .then(data => {
                closeModal();
                fetchRuns();
                if (data.run_id) {
                    selectRun(data.run_id);
                }
            })
            .catch(err => {
                alert("Failed to submit run: " + err);
            });
        }
    </script>
</body>
</html>
`
