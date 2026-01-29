# Semspec Web UI Specification

**Version**: 1.0.0  
**Status**: Draft  
**Last Updated**: 2025-01-28

---

## Table of Contents

1. [Overview](#1-overview)
2. [Architecture](#2-architecture)
3. [Design Language](#3-design-language)
4. [Views](#4-views)
5. [Components](#5-components)
6. [Data Layer](#6-data-layer)
7. [Real-time Updates](#7-real-time-updates)
8. [API Integration](#8-api-integration)
9. [Build & Deployment](#9-build--deployment)

---

## 1. Overview

### 1.1 Purpose

Web interface for Semspec providing:
- Chat-based interaction with agentic system
- Dashboard for monitoring loops, tasks, and system health
- Task management (proposals, specs, tasks)
- History and trajectory inspection
- Configuration management

### 1.2 Design Principles

- **Local-first**: Works entirely with local services (no cloud dependencies)
- **Real-time**: Live updates via SSE from service manager
- **Consistent**: Extends semstreams-ui design language
- **Accessible**: Keyboard navigable, screen reader friendly
- **Minimal dependencies**: No CSS frameworks, vanilla CSS with custom properties

### 1.3 Tech Stack

| Layer | Technology |
|-------|------------|
| Framework | SvelteKit 2 / Svelte 5 (runes) |
| Styling | Vanilla CSS with custom properties |
| Icons | Lucide (same as semstreams-ui) |
| Real-time | Server-Sent Events (SSE) |
| HTTP | Fetch API |

---

## 2. Architecture

### 2.1 System Context

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                                                              │
│  SEMSPEC WEB UI                      SERVICE MANAGER                        │
│  (SvelteKit)                         (Go HTTP Server)                       │
│                                                                              │
│  ┌──────────────────┐               ┌──────────────────┐                   │
│  │                  │   REST API    │                  │                   │
│  │  Views           │◄─────────────►│  /api/router/*   │                   │
│  │  • Chat          │               │  /api/agentic-*  │                   │
│  │  • Dashboard     │               │                  │                   │
│  │  • Tasks         │   SSE         │  /stream/activity│                   │
│  │  • History       │◄──────────────│                  │                   │
│  │  • Settings      │               │                  │                   │
│  │                  │               └────────┬─────────┘                   │
│  └──────────────────┘                        │                              │
│                                              │                              │
│                                    ┌─────────▼─────────┐                   │
│                                    │                   │                   │
│                                    │  Router, Loop,    │                   │
│                                    │  Model, Tools     │                   │
│                                    │  (Components)     │                   │
│                                    │                   │                   │
│                                    └───────────────────┘                   │
│                                                                              │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Project Structure

```
semspec-ui/
├── src/
│   ├── lib/
│   │   ├── api/
│   │   │   ├── client.ts           # HTTP client wrapper
│   │   │   ├── router.ts           # Router API endpoints
│   │   │   ├── loops.ts            # Loop API endpoints
│   │   │   ├── tasks.ts            # Task/proposal endpoints
│   │   │   └── system.ts           # System health endpoints
│   │   │
│   │   ├── stores/
│   │   │   ├── activity.svelte.ts  # SSE activity stream
│   │   │   ├── loops.svelte.ts     # Active loops state
│   │   │   ├── messages.svelte.ts  # Chat messages
│   │   │   ├── tasks.svelte.ts     # Tasks/proposals
│   │   │   └── system.svelte.ts    # System health
│   │   │
│   │   ├── components/
│   │   │   ├── chat/
│   │   │   │   ├── MessageInput.svelte
│   │   │   │   ├── MessageList.svelte
│   │   │   │   ├── Message.svelte
│   │   │   │   ├── LoopStatus.svelte
│   │   │   │   ├── ApprovalPrompt.svelte
│   │   │   │   └── CommandPalette.svelte
│   │   │   │
│   │   │   ├── dashboard/
│   │   │   │   ├── LoopTable.svelte
│   │   │   │   ├── ActivityFeed.svelte
│   │   │   │   ├── SystemHealth.svelte
│   │   │   │   └── StatsCards.svelte
│   │   │   │
│   │   │   ├── tasks/
│   │   │   │   ├── ProposalTree.svelte
│   │   │   │   ├── TaskDetail.svelte
│   │   │   │   ├── SpecViewer.svelte
│   │   │   │   └── NewProposal.svelte
│   │   │   │
│   │   │   ├── history/
│   │   │   │   ├── LoopHistory.svelte
│   │   │   │   ├── TrajectoryViewer.svelte
│   │   │   │   ├── TrajectoryStep.svelte
│   │   │   │   └── ExportPanel.svelte
│   │   │   │
│   │   │   ├── settings/
│   │   │   │   ├── ProjectInfo.svelte
│   │   │   │   ├── ConstitutionEditor.svelte
│   │   │   │   └── ModelConfig.svelte
│   │   │   │
│   │   │   └── shared/
│   │   │       ├── Sidebar.svelte
│   │   │       ├── Header.svelte
│   │   │       ├── StatusBadge.svelte
│   │   │       ├── DiffViewer.svelte
│   │   │       ├── CodeBlock.svelte
│   │   │       ├── Modal.svelte
│   │   │       ├── Tabs.svelte
│   │   │       └── Icon.svelte
│   │   │
│   │   └── utils/
│   │       ├── format.ts           # Formatting helpers
│   │       ├── time.ts             # Time/duration helpers
│   │       └── keyboard.ts         # Keyboard shortcuts
│   │
│   ├── routes/
│   │   ├── +layout.svelte          # App shell
│   │   ├── +page.svelte            # Chat (default)
│   │   ├── dashboard/
│   │   │   └── +page.svelte
│   │   ├── tasks/
│   │   │   ├── +page.svelte
│   │   │   └── [id]/
│   │   │       └── +page.svelte
│   │   ├── history/
│   │   │   ├── +page.svelte
│   │   │   └── [id]/
│   │   │       └── +page.svelte
│   │   └── settings/
│   │       └── +page.svelte
│   │
│   └── app.css                     # Global styles & design tokens
│
├── static/
│   └── favicon.svg
│
├── package.json
├── svelte.config.js
├── vite.config.ts
└── tsconfig.json
```

---

## 3. Design Language

Extends semstreams-ui design system. No external CSS frameworks.

### 3.1 CSS Custom Properties

```css
/* app.css - Design Tokens */

:root {
  /* Colors - Base */
  --color-bg-primary: #0f0f0f;
  --color-bg-secondary: #1a1a1a;
  --color-bg-tertiary: #252525;
  --color-bg-elevated: #2a2a2a;
  
  --color-text-primary: #e5e5e5;
  --color-text-secondary: #a0a0a0;
  --color-text-muted: #666666;
  
  --color-border: #333333;
  --color-border-focus: #555555;
  
  /* Colors - Semantic */
  --color-accent: #3b82f6;
  --color-accent-hover: #2563eb;
  --color-accent-muted: #1e3a5f;
  
  --color-success: #22c55e;
  --color-success-muted: #14532d;
  
  --color-warning: #f59e0b;
  --color-warning-muted: #78350f;
  
  --color-error: #ef4444;
  --color-error-muted: #7f1d1d;
  
  --color-info: #06b6d4;
  --color-info-muted: #164e63;
  
  /* Typography */
  --font-family-base: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  --font-family-mono: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  
  --font-size-xs: 0.75rem;    /* 12px */
  --font-size-sm: 0.875rem;   /* 14px */
  --font-size-base: 1rem;     /* 16px */
  --font-size-lg: 1.125rem;   /* 18px */
  --font-size-xl: 1.25rem;    /* 20px */
  --font-size-2xl: 1.5rem;    /* 24px */
  
  --font-weight-normal: 400;
  --font-weight-medium: 500;
  --font-weight-semibold: 600;
  
  --line-height-tight: 1.25;
  --line-height-normal: 1.5;
  --line-height-relaxed: 1.75;
  
  /* Spacing */
  --space-1: 0.25rem;   /* 4px */
  --space-2: 0.5rem;    /* 8px */
  --space-3: 0.75rem;   /* 12px */
  --space-4: 1rem;      /* 16px */
  --space-5: 1.25rem;   /* 20px */
  --space-6: 1.5rem;    /* 24px */
  --space-8: 2rem;      /* 32px */
  --space-10: 2.5rem;   /* 40px */
  --space-12: 3rem;     /* 48px */
  
  /* Border Radius */
  --radius-sm: 0.25rem;
  --radius-md: 0.375rem;
  --radius-lg: 0.5rem;
  --radius-xl: 0.75rem;
  --radius-full: 9999px;
  
  /* Shadows */
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.3);
  --shadow-md: 0 4px 6px rgba(0, 0, 0, 0.3);
  --shadow-lg: 0 10px 15px rgba(0, 0, 0, 0.3);
  
  /* Transitions */
  --transition-fast: 100ms ease;
  --transition-base: 200ms ease;
  --transition-slow: 300ms ease;
  
  /* Z-Index */
  --z-dropdown: 100;
  --z-modal: 200;
  --z-tooltip: 300;
  
  /* Layout */
  --sidebar-width: 240px;
  --header-height: 56px;
  --chat-max-width: 800px;
}

/* Light theme (optional, user preference) */
@media (prefers-color-scheme: light) {
  :root.auto-theme {
    --color-bg-primary: #ffffff;
    --color-bg-secondary: #f5f5f5;
    --color-bg-tertiary: #ebebeb;
    --color-bg-elevated: #ffffff;
    
    --color-text-primary: #1a1a1a;
    --color-text-secondary: #525252;
    --color-text-muted: #a0a0a0;
    
    --color-border: #e0e0e0;
    --color-border-focus: #c0c0c0;
  }
}
```

### 3.2 Base Styles

```css
/* app.css - Base Styles */

*,
*::before,
*::after {
  box-sizing: border-box;
  margin: 0;
  padding: 0;
}

html {
  font-size: 16px;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

body {
  font-family: var(--font-family-base);
  font-size: var(--font-size-base);
  line-height: var(--line-height-normal);
  color: var(--color-text-primary);
  background-color: var(--color-bg-primary);
}

code, pre, kbd {
  font-family: var(--font-family-mono);
}

a {
  color: var(--color-accent);
  text-decoration: none;
}

a:hover {
  text-decoration: underline;
}

button {
  font-family: inherit;
  font-size: inherit;
  cursor: pointer;
}

/* Focus styles */
:focus-visible {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}

/* Scrollbar */
::-webkit-scrollbar {
  width: 8px;
  height: 8px;
}

::-webkit-scrollbar-track {
  background: var(--color-bg-secondary);
}

::-webkit-scrollbar-thumb {
  background: var(--color-border);
  border-radius: var(--radius-full);
}

::-webkit-scrollbar-thumb:hover {
  background: var(--color-border-focus);
}
```

### 3.3 Component Patterns

```css
/* Buttons */
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-4);
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-medium);
  border-radius: var(--radius-md);
  border: 1px solid transparent;
  transition: all var(--transition-fast);
}

.btn-primary {
  background: var(--color-accent);
  color: white;
}

.btn-primary:hover {
  background: var(--color-accent-hover);
}

.btn-secondary {
  background: var(--color-bg-tertiary);
  color: var(--color-text-primary);
  border-color: var(--color-border);
}

.btn-secondary:hover {
  background: var(--color-bg-elevated);
}

.btn-ghost {
  background: transparent;
  color: var(--color-text-secondary);
}

.btn-ghost:hover {
  background: var(--color-bg-tertiary);
  color: var(--color-text-primary);
}

.btn-danger {
  background: var(--color-error);
  color: white;
}

.btn-danger:hover {
  background: #dc2626;
}

.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* Inputs */
.input {
  width: 100%;
  padding: var(--space-2) var(--space-3);
  font-size: var(--font-size-sm);
  color: var(--color-text-primary);
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-md);
  transition: border-color var(--transition-fast);
}

.input:focus {
  border-color: var(--color-accent);
  outline: none;
}

.input::placeholder {
  color: var(--color-text-muted);
}

/* Cards */
.card {
  background: var(--color-bg-secondary);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-lg);
  padding: var(--space-4);
}

.card-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: var(--space-3);
}

.card-title {
  font-size: var(--font-size-sm);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

/* Tables */
.table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--font-size-sm);
}

.table th,
.table td {
  padding: var(--space-3);
  text-align: left;
  border-bottom: 1px solid var(--color-border);
}

.table th {
  font-weight: var(--font-weight-medium);
  color: var(--color-text-secondary);
  background: var(--color-bg-tertiary);
}

.table tr:hover {
  background: var(--color-bg-tertiary);
}

/* Status badges */
.badge {
  display: inline-flex;
  align-items: center;
  gap: var(--space-1);
  padding: var(--space-1) var(--space-2);
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-medium);
  border-radius: var(--radius-full);
}

.badge-success {
  background: var(--color-success-muted);
  color: var(--color-success);
}

.badge-warning {
  background: var(--color-warning-muted);
  color: var(--color-warning);
}

.badge-error {
  background: var(--color-error-muted);
  color: var(--color-error);
}

.badge-info {
  background: var(--color-info-muted);
  color: var(--color-info);
}

.badge-neutral {
  background: var(--color-bg-tertiary);
  color: var(--color-text-secondary);
}
```

---

## 4. Views

### 4.1 Layout Shell

```svelte
<!-- src/routes/+layout.svelte -->
<script lang="ts">
  import { page } from '$app/stores';
  import Sidebar from '$lib/components/shared/Sidebar.svelte';
  import Header from '$lib/components/shared/Header.svelte';
  import { activityStore } from '$lib/stores/activity.svelte';
  
  // Initialize SSE connection on mount
  $effect(() => {
    activityStore.connect();
    return () => activityStore.disconnect();
  });
</script>

<div class="app-layout">
  <Sidebar currentPath={$page.url.pathname} />
  
  <div class="main-area">
    <Header />
    
    <main class="content">
      <slot />
    </main>
  </div>
</div>

<style>
  .app-layout {
    display: flex;
    height: 100vh;
    overflow: hidden;
  }
  
  .main-area {
    flex: 1;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  
  .content {
    flex: 1;
    overflow: auto;
  }
</style>
```

### 4.2 Chat View (Primary)

```svelte
<!-- src/routes/+page.svelte -->
<script lang="ts">
  import MessageList from '$lib/components/chat/MessageList.svelte';
  import MessageInput from '$lib/components/chat/MessageInput.svelte';
  import LoopSidebar from '$lib/components/chat/LoopSidebar.svelte';
  import { messagesStore } from '$lib/stores/messages.svelte';
  import { loopsStore } from '$lib/stores/loops.svelte';
  
  let showLoopSidebar = $state(true);
</script>

<div class="chat-view">
  <div class="chat-main">
    <MessageList messages={messagesStore.messages} />
    <MessageInput onSend={messagesStore.send} />
  </div>
  
  {#if showLoopSidebar}
    <LoopSidebar 
      loops={loopsStore.active} 
      onClose={() => showLoopSidebar = false}
    />
  {/if}
</div>

<style>
  .chat-view {
    display: flex;
    height: 100%;
  }
  
  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    max-width: var(--chat-max-width);
    margin: 0 auto;
    padding: var(--space-4);
  }
</style>
```

### 4.3 Dashboard View

```svelte
<!-- src/routes/dashboard/+page.svelte -->
<script lang="ts">
  import StatsCards from '$lib/components/dashboard/StatsCards.svelte';
  import LoopTable from '$lib/components/dashboard/LoopTable.svelte';
  import ActivityFeed from '$lib/components/dashboard/ActivityFeed.svelte';
  import SystemHealth from '$lib/components/dashboard/SystemHealth.svelte';
  import { loopsStore } from '$lib/stores/loops.svelte';
  import { activityStore } from '$lib/stores/activity.svelte';
  import { systemStore } from '$lib/stores/system.svelte';
</script>

<div class="dashboard">
  <h1 class="page-title">Dashboard</h1>
  
  <StatsCards 
    activeLoops={loopsStore.active.length}
    pendingReview={loopsStore.pendingReview.length}
    completedToday={loopsStore.completedToday}
  />
  
  <div class="dashboard-grid">
    <section class="card">
      <div class="card-header">
        <h2 class="card-title">Active Loops</h2>
      </div>
      <LoopTable loops={loopsStore.active} />
    </section>
    
    <section class="card">
      <div class="card-header">
        <h2 class="card-title">Live Activity</h2>
      </div>
      <ActivityFeed events={activityStore.recent} />
    </section>
  </div>
  
  <section class="card">
    <div class="card-header">
      <h2 class="card-title">System Health</h2>
    </div>
    <SystemHealth status={systemStore.status} />
  </section>
</div>

<style>
  .dashboard {
    padding: var(--space-6);
    max-width: 1400px;
    margin: 0 auto;
  }
  
  .page-title {
    font-size: var(--font-size-2xl);
    font-weight: var(--font-weight-semibold);
    margin-bottom: var(--space-6);
  }
  
  .dashboard-grid {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: var(--space-4);
    margin-bottom: var(--space-4);
  }
  
  @media (max-width: 1024px) {
    .dashboard-grid {
      grid-template-columns: 1fr;
    }
  }
</style>
```

### 4.4 Tasks View

```svelte
<!-- src/routes/tasks/+page.svelte -->
<script lang="ts">
  import ProposalTree from '$lib/components/tasks/ProposalTree.svelte';
  import TaskDetail from '$lib/components/tasks/TaskDetail.svelte';
  import NewProposal from '$lib/components/tasks/NewProposal.svelte';
  import Modal from '$lib/components/shared/Modal.svelte';
  import { tasksStore } from '$lib/stores/tasks.svelte';
  
  let selectedTask = $state<string | null>(null);
  let showNewProposal = $state(false);
  let filterStatus = $state('all');
  
  const filteredTasks = $derived(
    filterStatus === 'all' 
      ? tasksStore.proposals 
      : tasksStore.proposals.filter(p => p.status === filterStatus)
  );
</script>

<div class="tasks-view">
  <div class="tasks-header">
    <h1 class="page-title">Tasks</h1>
    <button class="btn btn-primary" onclick={() => showNewProposal = true}>
      + New Proposal
    </button>
  </div>
  
  <div class="tasks-toolbar">
    <select class="input" bind:value={filterStatus} style="width: auto;">
      <option value="all">All Status</option>
      <option value="exploring">Exploring</option>
      <option value="drafted">Drafted</option>
      <option value="approved">Approved</option>
      <option value="implementing">Implementing</option>
      <option value="complete">Complete</option>
    </select>
  </div>
  
  <div class="tasks-layout">
    <div class="tasks-list">
      <ProposalTree 
        proposals={filteredTasks} 
        selected={selectedTask}
        onSelect={(id) => selectedTask = id}
      />
    </div>
    
    {#if selectedTask}
      <div class="task-detail">
        <TaskDetail 
          taskId={selectedTask} 
          onClose={() => selectedTask = null}
        />
      </div>
    {/if}
  </div>
  
  {#if showNewProposal}
    <Modal onClose={() => showNewProposal = false}>
      <NewProposal onSubmit={() => showNewProposal = false} />
    </Modal>
  {/if}
</div>

<style>
  .tasks-view {
    padding: var(--space-6);
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  
  .tasks-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-4);
  }
  
  .page-title {
    font-size: var(--font-size-2xl);
    font-weight: var(--font-weight-semibold);
  }
  
  .tasks-toolbar {
    margin-bottom: var(--space-4);
  }
  
  .tasks-layout {
    flex: 1;
    display: flex;
    gap: var(--space-4);
    overflow: hidden;
  }
  
  .tasks-list {
    flex: 1;
    overflow: auto;
  }
  
  .task-detail {
    width: 480px;
    flex-shrink: 0;
    overflow: auto;
  }
</style>
```

### 4.5 History View

```svelte
<!-- src/routes/history/+page.svelte -->
<script lang="ts">
  import LoopHistory from '$lib/components/history/LoopHistory.svelte';
  import TrajectoryViewer from '$lib/components/history/TrajectoryViewer.svelte';
  import ExportPanel from '$lib/components/history/ExportPanel.svelte';
  import { api } from '$lib/api/client';
  
  let selectedLoop = $state<string | null>(null);
  let trajectory = $state(null);
  let dateRange = $state('7d');
  let outcomeFilter = $state('all');
  let showExport = $state(false);
  
  async function loadTrajectory(loopId: string) {
    selectedLoop = loopId;
    trajectory = await api.loops.getTrajectory(loopId);
  }
</script>

<div class="history-view">
  <div class="history-header">
    <h1 class="page-title">History</h1>
    <button class="btn btn-secondary" onclick={() => showExport = true}>
      Export Training Data
    </button>
  </div>
  
  <div class="history-toolbar">
    <select class="input" bind:value={dateRange} style="width: auto;">
      <option value="1d">Last 24 hours</option>
      <option value="7d">Last 7 days</option>
      <option value="30d">Last 30 days</option>
      <option value="all">All time</option>
    </select>
    
    <select class="input" bind:value={outcomeFilter} style="width: auto;">
      <option value="all">All outcomes</option>
      <option value="complete">Complete</option>
      <option value="approved">Approved</option>
      <option value="failed">Failed</option>
      <option value="cancelled">Cancelled</option>
    </select>
  </div>
  
  <div class="history-layout">
    <div class="history-list">
      <LoopHistory 
        {dateRange}
        {outcomeFilter}
        selected={selectedLoop}
        onSelect={loadTrajectory}
      />
    </div>
    
    {#if selectedLoop && trajectory}
      <div class="trajectory-panel">
        <TrajectoryViewer 
          {trajectory} 
          onClose={() => { selectedLoop = null; trajectory = null; }}
        />
      </div>
    {/if}
  </div>
  
  {#if showExport}
    <ExportPanel onClose={() => showExport = false} />
  {/if}
</div>

<style>
  .history-view {
    padding: var(--space-6);
    height: 100%;
    display: flex;
    flex-direction: column;
  }
  
  .history-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--space-4);
  }
  
  .page-title {
    font-size: var(--font-size-2xl);
    font-weight: var(--font-weight-semibold);
  }
  
  .history-toolbar {
    display: flex;
    gap: var(--space-3);
    margin-bottom: var(--space-4);
  }
  
  .history-layout {
    flex: 1;
    display: flex;
    gap: var(--space-4);
    overflow: hidden;
  }
  
  .history-list {
    flex: 1;
    overflow: auto;
  }
  
  .trajectory-panel {
    width: 600px;
    flex-shrink: 0;
    overflow: auto;
  }
</style>
```

### 4.6 Settings View

```svelte
<!-- src/routes/settings/+page.svelte -->
<script lang="ts">
  import ProjectInfo from '$lib/components/settings/ProjectInfo.svelte';
  import ConstitutionEditor from '$lib/components/settings/ConstitutionEditor.svelte';
  import ModelConfig from '$lib/components/settings/ModelConfig.svelte';
  import Tabs from '$lib/components/shared/Tabs.svelte';
  
  let activeTab = $state('project');
  
  const tabs = [
    { id: 'project', label: 'Project' },
    { id: 'constitution', label: 'Constitution' },
    { id: 'models', label: 'Models' },
  ];
</script>

<div class="settings-view">
  <h1 class="page-title">Settings</h1>
  
  <Tabs {tabs} bind:active={activeTab} />
  
  <div class="settings-content">
    {#if activeTab === 'project'}
      <ProjectInfo />
    {:else if activeTab === 'constitution'}
      <ConstitutionEditor />
    {:else if activeTab === 'models'}
      <ModelConfig />
    {/if}
  </div>
</div>

<style>
  .settings-view {
    padding: var(--space-6);
    max-width: 800px;
  }
  
  .page-title {
    font-size: var(--font-size-2xl);
    font-weight: var(--font-weight-semibold);
    margin-bottom: var(--space-4);
  }
  
  .settings-content {
    margin-top: var(--space-6);
  }
</style>
```

---

## 5. Components

### 5.1 Chat Components

#### MessageInput

```svelte
<!-- src/lib/components/chat/MessageInput.svelte -->
<script lang="ts">
  import Icon from '$lib/components/shared/Icon.svelte';
  import CommandPalette from './CommandPalette.svelte';
  
  interface Props {
    onSend: (content: string) => Promise<void>;
  }
  
  let { onSend }: Props = $props();
  
  let input = $state('');
  let sending = $state(false);
  let showCommands = $state(false);
  let textarea: HTMLTextAreaElement;
  
  async function send() {
    if (!input.trim() || sending) return;
    
    sending = true;
    try {
      await onSend(input);
      input = '';
      // Reset textarea height
      if (textarea) {
        textarea.style.height = 'auto';
      }
    } finally {
      sending = false;
    }
  }
  
  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      send();
    }
    
    // Show command palette on /
    if (e.key === '/' && input === '') {
      showCommands = true;
    }
    
    // Close command palette on Escape
    if (e.key === 'Escape') {
      showCommands = false;
    }
  }
  
  function handleInput() {
    // Auto-resize textarea
    if (textarea) {
      textarea.style.height = 'auto';
      textarea.style.height = Math.min(textarea.scrollHeight, 200) + 'px';
    }
    
    // Hide commands if not starting with /
    if (!input.startsWith('/')) {
      showCommands = false;
    }
  }
  
  function selectCommand(cmd: string) {
    input = '/' + cmd + ' ';
    showCommands = false;
    textarea?.focus();
  }
</script>

<div class="message-input-container">
  {#if showCommands}
    <CommandPalette 
      filter={input.slice(1)} 
      onSelect={selectCommand}
      onClose={() => showCommands = false}
    />
  {/if}
  
  <div class="message-input">
    <textarea
      bind:this={textarea}
      bind:value={input}
      oninput={handleInput}
      onkeydown={handleKeydown}
      placeholder="Type a message or / for commands..."
      rows="1"
      disabled={sending}
    />
    
    <button 
      class="send-button"
      onclick={send}
      disabled={sending || !input.trim()}
      aria-label="Send message"
    >
      <Icon name={sending ? 'loader' : 'send'} size={20} />
    </button>
  </div>
</div>

<style>
  .message-input-container {
    position: relative;
  }
  
  .message-input {
    display: flex;
    align-items: flex-end;
    gap: var(--space-2);
    padding: var(--space-3);
    background: var(--color-bg-secondary);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
  }
  
  textarea {
    flex: 1;
    resize: none;
    border: none;
    background: transparent;
    color: var(--color-text-primary);
    font-family: inherit;
    font-size: var(--font-size-base);
    line-height: var(--line-height-normal);
    min-height: 24px;
    max-height: 200px;
  }
  
  textarea:focus {
    outline: none;
  }
  
  textarea::placeholder {
    color: var(--color-text-muted);
  }
  
  .send-button {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    background: var(--color-accent);
    color: white;
    border: none;
    border-radius: var(--radius-md);
    transition: background var(--transition-fast);
  }
  
  .send-button:hover:not(:disabled) {
    background: var(--color-accent-hover);
  }
  
  .send-button:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
</style>
```

#### LoopStatus

```svelte
<!-- src/lib/components/chat/LoopStatus.svelte -->
<script lang="ts">
  import Icon from '$lib/components/shared/Icon.svelte';
  import StatusBadge from '$lib/components/shared/StatusBadge.svelte';
  import type { Loop } from '$lib/types';
  
  interface Props {
    loop: Loop;
    expanded?: boolean;
  }
  
  let { loop, expanded = false }: Props = $props();
  
  let isExpanded = $state(expanded);
  
  const stateIcons: Record<string, string> = {
    executing: 'play',
    paused: 'pause',
    awaiting_approval: 'clock',
    complete: 'check',
    failed: 'x',
    cancelled: 'slash',
  };
</script>

<div class="loop-status" class:expanded={isExpanded}>
  <button class="loop-header" onclick={() => isExpanded = !isExpanded}>
    <div class="loop-info">
      <Icon name={stateIcons[loop.state] || 'circle'} size={14} />
      <span class="loop-id">loop:{loop.id.slice(0, 8)}</span>
      <StatusBadge status={loop.state} />
    </div>
    
    <div class="loop-meta">
      <span class="iterations">{loop.iterations}/{loop.maxIterations}</span>
      <Icon name={isExpanded ? 'chevron-up' : 'chevron-down'} size={14} />
    </div>
  </button>
  
  {#if isExpanded}
    <div class="loop-details">
      <div class="detail-row">
        <span class="label">Role:</span>
        <span class="value">{loop.role}</span>
      </div>
      <div class="detail-row">
        <span class="label">Model:</span>
        <span class="value">{loop.model}</span>
      </div>
      {#if loop.pendingTools.length > 0}
        <div class="detail-row">
          <span class="label">Pending:</span>
          <span class="value">{loop.pendingTools.join(', ')}</span>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .loop-status {
    background: var(--color-bg-tertiary);
    border-radius: var(--radius-md);
    margin: var(--space-2) 0;
    font-size: var(--font-size-sm);
  }
  
  .loop-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
    padding: var(--space-2) var(--space-3);
    background: none;
    border: none;
    color: var(--color-text-secondary);
    text-align: left;
  }
  
  .loop-header:hover {
    color: var(--color-text-primary);
  }
  
  .loop-info {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  
  .loop-id {
    font-family: var(--font-family-mono);
  }
  
  .loop-meta {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  
  .iterations {
    font-family: var(--font-family-mono);
    color: var(--color-text-muted);
  }
  
  .loop-details {
    padding: var(--space-2) var(--space-3) var(--space-3);
    border-top: 1px solid var(--color-border);
  }
  
  .detail-row {
    display: flex;
    gap: var(--space-2);
    margin-top: var(--space-1);
  }
  
  .label {
    color: var(--color-text-muted);
  }
  
  .value {
    color: var(--color-text-primary);
    font-family: var(--font-family-mono);
  }
</style>
```

#### ApprovalPrompt

```svelte
<!-- src/lib/components/chat/ApprovalPrompt.svelte -->
<script lang="ts">
  import Icon from '$lib/components/shared/Icon.svelte';
  import DiffViewer from '$lib/components/shared/DiffViewer.svelte';
  import type { Loop, CompletionResult } from '$lib/types';
  
  interface Props {
    loop: Loop;
    result: CompletionResult;
    onApprove: () => void;
    onReject: (reason?: string) => void;
  }
  
  let { loop, result, onApprove, onReject }: Props = $props();
  
  let showDiff = $state(false);
  let rejectReason = $state('');
  let showRejectForm = $state(false);
  
  function handleReject() {
    if (showRejectForm) {
      onReject(rejectReason || undefined);
    } else {
      showRejectForm = true;
    }
  }
</script>

<div class="approval-prompt">
  <div class="prompt-header">
    <Icon name="clock" size={16} />
    <span>Ready for review</span>
  </div>
  
  <div class="prompt-content">
    <p class="summary">{result.summary}</p>
    
    {#if result.filesChanged?.length > 0}
      <div class="files-changed">
        <span class="files-label">Files:</span>
        {#each result.filesChanged as file}
          <span class="file-badge" class:added={file.type === 'added'} class:modified={file.type === 'modified'}>
            {file.type === 'added' ? '+' : '~'} {file.path}
          </span>
        {/each}
      </div>
    {/if}
    
    {#if result.diff}
      <button class="toggle-diff" onclick={() => showDiff = !showDiff}>
        <Icon name={showDiff ? 'chevron-up' : 'chevron-down'} size={14} />
        {showDiff ? 'Hide' : 'Show'} diff
      </button>
      
      {#if showDiff}
        <DiffViewer diff={result.diff} />
      {/if}
    {/if}
  </div>
  
  {#if showRejectForm}
    <div class="reject-form">
      <textarea
        bind:value={rejectReason}
        placeholder="Reason for rejection (optional)..."
        rows="2"
        class="input"
      />
    </div>
  {/if}
  
  <div class="prompt-actions">
    <button class="btn btn-primary" onclick={onApprove}>
      <Icon name="check" size={16} />
      Approve
    </button>
    <button class="btn btn-danger" onclick={handleReject}>
      <Icon name="x" size={16} />
      {showRejectForm ? 'Confirm Reject' : 'Reject'}
    </button>
    {#if showRejectForm}
      <button class="btn btn-ghost" onclick={() => showRejectForm = false}>
        Cancel
      </button>
    {/if}
  </div>
</div>

<style>
  .approval-prompt {
    background: var(--color-bg-secondary);
    border: 1px solid var(--color-warning);
    border-radius: var(--radius-lg);
    padding: var(--space-4);
    margin: var(--space-3) 0;
  }
  
  .prompt-header {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-warning);
    font-weight: var(--font-weight-medium);
    margin-bottom: var(--space-3);
  }
  
  .prompt-content {
    margin-bottom: var(--space-4);
  }
  
  .summary {
    color: var(--color-text-primary);
    margin-bottom: var(--space-3);
  }
  
  .files-changed {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--font-size-sm);
    margin-bottom: var(--space-3);
  }
  
  .files-label {
    color: var(--color-text-muted);
  }
  
  .file-badge {
    font-family: var(--font-family-mono);
    font-size: var(--font-size-xs);
    padding: var(--space-1) var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-bg-tertiary);
  }
  
  .file-badge.added {
    color: var(--color-success);
  }
  
  .file-badge.modified {
    color: var(--color-warning);
  }
  
  .toggle-diff {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    background: none;
    border: none;
    color: var(--color-text-secondary);
    font-size: var(--font-size-sm);
    cursor: pointer;
    margin-bottom: var(--space-2);
  }
  
  .toggle-diff:hover {
    color: var(--color-text-primary);
  }
  
  .reject-form {
    margin-bottom: var(--space-3);
  }
  
  .prompt-actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
```

### 5.2 Shared Components

#### Sidebar

```svelte
<!-- src/lib/components/shared/Sidebar.svelte -->
<script lang="ts">
  import Icon from './Icon.svelte';
  import { loopsStore } from '$lib/stores/loops.svelte';
  import { systemStore } from '$lib/stores/system.svelte';
  
  interface Props {
    currentPath: string;
  }
  
  let { currentPath }: Props = $props();
  
  const navItems = [
    { path: '/', icon: 'message-square', label: 'Chat' },
    { path: '/dashboard', icon: 'layout-dashboard', label: 'Dashboard' },
    { path: '/tasks', icon: 'list-checks', label: 'Tasks' },
    { path: '/history', icon: 'history', label: 'History' },
    { path: '/settings', icon: 'settings', label: 'Settings' },
  ];
  
  function isActive(path: string): boolean {
    if (path === '/') return currentPath === '/';
    return currentPath.startsWith(path);
  }
</script>

<aside class="sidebar">
  <div class="sidebar-header">
    <span class="logo">Semspec</span>
  </div>
  
  <nav class="sidebar-nav">
    {#each navItems as item}
      <a 
        href={item.path} 
        class="nav-item"
        class:active={isActive(item.path)}
      >
        <Icon name={item.icon} size={20} />
        <span>{item.label}</span>
        
        {#if item.path === '/tasks' && loopsStore.pendingReview.length > 0}
          <span class="badge">{loopsStore.pendingReview.length}</span>
        {/if}
      </a>
    {/each}
  </nav>
  
  <div class="sidebar-footer">
    <div class="system-status">
      <div class="status-indicator" class:healthy={systemStore.healthy} />
      <span class="status-text">
        {systemStore.healthy ? 'System healthy' : 'System issues'}
      </span>
    </div>
    
    <div class="active-loops">
      <Icon name="activity" size={14} />
      <span>{loopsStore.active.length} active loops</span>
    </div>
  </div>
</aside>

<style>
  .sidebar {
    width: var(--sidebar-width);
    height: 100%;
    background: var(--color-bg-secondary);
    border-right: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
  }
  
  .sidebar-header {
    padding: var(--space-4);
    border-bottom: 1px solid var(--color-border);
  }
  
  .logo {
    font-size: var(--font-size-xl);
    font-weight: var(--font-weight-semibold);
    color: var(--color-text-primary);
  }
  
  .sidebar-nav {
    flex: 1;
    padding: var(--space-2);
  }
  
  .nav-item {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-3);
    color: var(--color-text-secondary);
    border-radius: var(--radius-md);
    text-decoration: none;
    transition: all var(--transition-fast);
  }
  
  .nav-item:hover {
    background: var(--color-bg-tertiary);
    color: var(--color-text-primary);
  }
  
  .nav-item.active {
    background: var(--color-accent-muted);
    color: var(--color-accent);
  }
  
  .nav-item .badge {
    margin-left: auto;
    background: var(--color-warning);
    color: var(--color-bg-primary);
    font-size: var(--font-size-xs);
    font-weight: var(--font-weight-semibold);
    padding: 2px 6px;
    border-radius: var(--radius-full);
  }
  
  .sidebar-footer {
    padding: var(--space-4);
    border-top: 1px solid var(--color-border);
    font-size: var(--font-size-sm);
  }
  
  .system-status {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin-bottom: var(--space-2);
  }
  
  .status-indicator {
    width: 8px;
    height: 8px;
    border-radius: var(--radius-full);
    background: var(--color-error);
  }
  
  .status-indicator.healthy {
    background: var(--color-success);
  }
  
  .status-text {
    color: var(--color-text-muted);
  }
  
  .active-loops {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--color-text-muted);
  }
</style>
```

#### StatusBadge

```svelte
<!-- src/lib/components/shared/StatusBadge.svelte -->
<script lang="ts">
  interface Props {
    status: string;
    size?: 'sm' | 'md';
  }
  
  let { status, size = 'sm' }: Props = $props();
  
  const statusConfig: Record<string, { class: string; label: string }> = {
    executing: { class: 'info', label: 'Executing' },
    exploring: { class: 'info', label: 'Exploring' },
    paused: { class: 'warning', label: 'Paused' },
    awaiting_approval: { class: 'warning', label: 'Review' },
    complete: { class: 'success', label: 'Complete' },
    approved: { class: 'success', label: 'Approved' },
    implementing: { class: 'info', label: 'Implementing' },
    drafted: { class: 'neutral', label: 'Drafted' },
    failed: { class: 'error', label: 'Failed' },
    cancelled: { class: 'neutral', label: 'Cancelled' },
  };
  
  const config = $derived(statusConfig[status] || { class: 'neutral', label: status });
</script>

<span class="badge badge-{config.class}" class:sm={size === 'sm'}>
  {config.label}
</span>

<style>
  .badge {
    display: inline-flex;
    align-items: center;
    padding: var(--space-1) var(--space-2);
    font-size: var(--font-size-xs);
    font-weight: var(--font-weight-medium);
    border-radius: var(--radius-full);
    text-transform: capitalize;
  }
  
  .badge.sm {
    padding: 2px var(--space-2);
    font-size: 10px;
  }
  
  .badge-success {
    background: var(--color-success-muted);
    color: var(--color-success);
  }
  
  .badge-warning {
    background: var(--color-warning-muted);
    color: var(--color-warning);
  }
  
  .badge-error {
    background: var(--color-error-muted);
    color: var(--color-error);
  }
  
  .badge-info {
    background: var(--color-info-muted);
    color: var(--color-info);
  }
  
  .badge-neutral {
    background: var(--color-bg-tertiary);
    color: var(--color-text-secondary);
  }
</style>
```

---

## 6. Data Layer

### 6.1 Stores (Svelte 5 Runes)

#### Activity Store (SSE)

```typescript
// src/lib/stores/activity.svelte.ts
import { browser } from '$app/environment';

export interface ActivityEvent {
  type: 'loop_started' | 'loop_complete' | 'model_request' | 'model_response' | 
        'tool_call' | 'tool_result' | 'status_update';
  loop_id: string;
  timestamp: string;
  data: Record<string, unknown>;
}

class ActivityStore {
  recent = $state<ActivityEvent[]>([]);
  connected = $state(false);
  
  private eventSource: EventSource | null = null;
  private maxEvents = 100;
  
  connect(filter?: string) {
    if (!browser) return;
    
    const url = filter 
      ? `/stream/activity?filter=${encodeURIComponent(filter)}`
      : '/stream/activity';
    
    this.eventSource = new EventSource(url);
    
    this.eventSource.onopen = () => {
      this.connected = true;
    };
    
    this.eventSource.onmessage = (event) => {
      const activity = JSON.parse(event.data) as ActivityEvent;
      this.recent = [...this.recent.slice(-(this.maxEvents - 1)), activity];
    };
    
    this.eventSource.onerror = () => {
      this.connected = false;
      // Reconnect after delay
      setTimeout(() => this.connect(filter), 3000);
    };
  }
  
  disconnect() {
    this.eventSource?.close();
    this.eventSource = null;
    this.connected = false;
  }
  
  clear() {
    this.recent = [];
  }
}

export const activityStore = new ActivityStore();
```

#### Loops Store

```typescript
// src/lib/stores/loops.svelte.ts
import { api } from '$lib/api/client';
import { activityStore } from './activity.svelte';

export interface Loop {
  id: string;
  owner: string;
  source: string;
  state: string;
  role: string;
  model: string;
  iterations: number;
  maxIterations: number;
  pendingTools: string[];
  startedAt: string;
  prompt: string;
}

class LoopsStore {
  all = $state<Loop[]>([]);
  loading = $state(false);
  error = $state<string | null>(null);
  
  // Derived states
  get active() {
    return this.all.filter(l => ['executing', 'paused', 'awaiting_approval'].includes(l.state));
  }
  
  get pendingReview() {
    return this.all.filter(l => l.state === 'awaiting_approval');
  }
  
  get completedToday() {
    const today = new Date().toDateString();
    return this.all.filter(l => 
      l.state === 'complete' && 
      new Date(l.startedAt).toDateString() === today
    ).length;
  }
  
  async fetch() {
    this.loading = true;
    this.error = null;
    
    try {
      const response = await api.router.getLoops();
      this.all = response.loops;
    } catch (err) {
      this.error = err instanceof Error ? err.message : 'Failed to fetch loops';
    } finally {
      this.loading = false;
    }
  }
  
  async sendSignal(loopId: string, type: string, payload?: unknown) {
    await api.router.sendSignal(loopId, type, payload);
  }
  
  // Update from SSE events
  handleActivity(event: ActivityEvent) {
    if (event.type === 'loop_started') {
      this.fetch(); // Refresh list
    } else if (event.type === 'loop_complete') {
      const index = this.all.findIndex(l => l.id === event.loop_id);
      if (index >= 0) {
        this.all[index] = { ...this.all[index], state: 'complete' };
      }
    } else if (event.type === 'status_update') {
      const index = this.all.findIndex(l => l.id === event.loop_id);
      if (index >= 0) {
        this.all[index] = { ...this.all[index], ...event.data };
      }
    }
  }
}

export const loopsStore = new LoopsStore();

// Subscribe to activity events
$effect.root(() => {
  $effect(() => {
    for (const event of activityStore.recent) {
      loopsStore.handleActivity(event);
    }
  });
});
```

#### Messages Store

```typescript
// src/lib/stores/messages.svelte.ts
import { api } from '$lib/api/client';

export interface Message {
  id: string;
  type: 'user' | 'assistant' | 'status' | 'error';
  content: string;
  timestamp: string;
  loopId?: string;
  actions?: Array<{
    id: string;
    label: string;
    signal: string;
    style: 'primary' | 'danger' | 'ghost';
  }>;
}

class MessagesStore {
  messages = $state<Message[]>([]);
  sending = $state(false);
  
  async send(content: string) {
    // Add user message immediately
    const userMessage: Message = {
      id: crypto.randomUUID(),
      type: 'user',
      content,
      timestamp: new Date().toISOString(),
    };
    
    this.messages = [...this.messages, userMessage];
    this.sending = true;
    
    try {
      const response = await api.router.sendMessage(content);
      
      // Add assistant response
      const assistantMessage: Message = {
        id: response.id,
        type: response.type,
        content: response.content,
        timestamp: response.timestamp,
        loopId: response.loopId,
        actions: response.actions,
      };
      
      this.messages = [...this.messages, assistantMessage];
    } catch (err) {
      // Add error message
      const errorMessage: Message = {
        id: crypto.randomUUID(),
        type: 'error',
        content: err instanceof Error ? err.message : 'Failed to send message',
        timestamp: new Date().toISOString(),
      };
      
      this.messages = [...this.messages, errorMessage];
    } finally {
      this.sending = false;
    }
  }
  
  async executeAction(loopId: string, signal: string, payload?: unknown) {
    await api.router.sendSignal(loopId, signal, payload);
  }
  
  clear() {
    this.messages = [];
  }
}

export const messagesStore = new MessagesStore();
```

---

## 7. Real-time Updates

### 7.1 SSE Connection

The UI connects to the service manager's SSE endpoint for real-time updates.

**Endpoint**: `GET /stream/activity`

**Query Parameters**:
- `filter`: Optional loop ID to filter events

**Event Format**:
```typescript
interface SSEEvent {
  type: string;
  loop_id: string;
  timestamp: string;
  data: Record<string, unknown>;
}
```

**Event Types**:

| Type | Description | Data Fields |
|------|-------------|-------------|
| `loop_started` | New loop created | `owner`, `source`, `prompt` |
| `loop_complete` | Loop finished | `outcome`, `duration_ms` |
| `model_request` | Model call started | `model`, `tokens_in` |
| `model_response` | Model call finished | `tokens_out`, `duration_ms` |
| `tool_call` | Tool execution started | `tool`, `args` |
| `tool_result` | Tool execution finished | `tool`, `duration_ms`, `success` |
| `status_update` | Loop state changed | `state`, `iterations` |

### 7.2 Connection Management

```typescript
// In +layout.svelte
import { activityStore } from '$lib/stores/activity.svelte';
import { loopsStore } from '$lib/stores/loops.svelte';
import { systemStore } from '$lib/stores/system.svelte';

// Connect on mount
$effect(() => {
  activityStore.connect();
  loopsStore.fetch();
  systemStore.fetch();
  
  // Periodic refresh for non-SSE data
  const interval = setInterval(() => {
    loopsStore.fetch();
    systemStore.fetch();
  }, 30000);
  
  return () => {
    activityStore.disconnect();
    clearInterval(interval);
  };
});
```

---

## 8. API Integration

### 8.1 HTTP Client

```typescript
// src/lib/api/client.ts
const BASE_URL = import.meta.env.VITE_API_URL || '';

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE';
  body?: unknown;
  headers?: Record<string, string>;
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { method = 'GET', body, headers = {} } = options;
  
  const response = await fetch(`${BASE_URL}${path}`, {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...headers,
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  
  if (!response.ok) {
    const error = await response.json().catch(() => ({ message: response.statusText }));
    throw new Error(error.message || `Request failed: ${response.status}`);
  }
  
  return response.json();
}

export const api = {
  router: {
    getLoops: (params?: { owner?: string; state?: string }) => 
      request<{ loops: Loop[]; total: number }>(`/api/router/loops${toQueryString(params)}`),
    
    getLoop: (id: string) => 
      request<Loop>(`/api/router/loops/${id}`),
    
    sendSignal: (loopId: string, type: string, payload?: unknown) =>
      request(`/api/router/loops/${loopId}/signal`, { 
        method: 'POST', 
        body: { type, payload } 
      }),
    
    sendMessage: (content: string) =>
      request<Message>('/api/router/message', { 
        method: 'POST', 
        body: { content } 
      }),
    
    getCommands: () =>
      request<{ commands: CommandConfig[] }>('/api/router/commands'),
  },
  
  loops: {
    getTrajectory: (id: string, format: 'json' | 'summary' = 'summary') =>
      request(`/api/agentic-loop/loops/${id}/trajectory?format=${format}`),
    
    getStats: () =>
      request('/api/agentic-loop/stats'),
  },
  
  model: {
    getStatus: () =>
      request('/api/agentic-model/status'),
    
    getStats: () =>
      request('/api/agentic-model/stats'),
  },
  
  tools: {
    getExecutors: () =>
      request('/api/agentic-tools/executors'),
    
    getStats: () =>
      request('/api/agentic-tools/stats'),
  },
  
  system: {
    getHealth: () =>
      request('/api/health'),
    
    getComponents: () =>
      request('/api/components'),
  },
};

function toQueryString(params?: Record<string, unknown>): string {
  if (!params) return '';
  const entries = Object.entries(params).filter(([, v]) => v !== undefined);
  if (entries.length === 0) return '';
  return '?' + new URLSearchParams(entries.map(([k, v]) => [k, String(v)])).toString();
}
```

### 8.2 API Endpoints Summary

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/health` | GET | System health |
| `/api/components` | GET | Component status |
| `/api/router/loops` | GET | List loops |
| `/api/router/loops/:id` | GET | Loop detail |
| `/api/router/loops/:id/signal` | POST | Send signal |
| `/api/router/message` | POST | Send chat message |
| `/api/router/commands` | GET | Available commands |
| `/api/agentic-loop/loops/:id/trajectory` | GET | Loop trajectory |
| `/api/agentic-loop/stats` | GET | Loop statistics |
| `/api/agentic-model/status` | GET | Model connectivity |
| `/api/agentic-model/stats` | GET | Model usage stats |
| `/api/agentic-tools/executors` | GET | Registered tools |
| `/api/agentic-tools/stats` | GET | Tool statistics |
| `/stream/activity` | SSE | Real-time events |

---

## 9. Build & Deployment

### 9.1 Package Configuration

```json
{
  "name": "semspec-ui",
  "version": "0.1.0",
  "private": true,
  "scripts": {
    "dev": "vite dev",
    "build": "vite build",
    "preview": "vite preview",
    "check": "svelte-kit sync && svelte-check --tsconfig ./tsconfig.json",
    "lint": "eslint ."
  },
  "devDependencies": {
    "@sveltejs/adapter-static": "^3.0.0",
    "@sveltejs/kit": "^2.0.0",
    "@sveltejs/vite-plugin-svelte": "^4.0.0",
    "svelte": "^5.0.0",
    "svelte-check": "^4.0.0",
    "typescript": "^5.0.0",
    "vite": "^6.0.0"
  },
  "dependencies": {
    "lucide-svelte": "^0.300.0"
  }
}
```

### 9.2 Vite Configuration

```typescript
// vite.config.ts
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/stream': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
});
```

### 9.3 Static Adapter

```javascript
// svelte.config.js
import adapter from '@sveltejs/adapter-static';

export default {
  kit: {
    adapter: adapter({
      pages: 'build',
      assets: 'build',
      fallback: 'index.html',
    }),
  },
};
```

### 9.4 Deployment Options

**Option 1: Embedded in Service Manager**

Service manager serves static files from build directory:

```go
// In service manager
mux.Handle("/", http.FileServer(http.Dir("./semspec-ui/build")))
```

**Option 2: Separate Server**

Run SvelteKit in preview mode or behind nginx:

```bash
npm run build
npm run preview -- --port 3000
```

### 9.5 Build Order

**Phase 1: Chat MVP**
1. Layout shell + sidebar
2. Message input/output
3. SSE connection
4. Loop status inline
5. Basic approve/reject

**Phase 2: Dashboard**
1. Stats cards
2. Loop table
3. Activity feed
4. System health

**Phase 3: Tasks & History**
1. Proposal tree
2. Task detail panel
3. History list
4. Trajectory viewer

**Phase 4: Polish**
1. Settings pages
2. Command palette
3. Keyboard shortcuts
4. Diff viewer
5. Export functionality

---

## Appendix A: Type Definitions

```typescript
// src/lib/types.ts

export interface Loop {
  id: string;
  owner: string;
  source: string;
  sourceId: string;
  channelType: string;
  channelId: string;
  state: LoopState;
  role: string;
  model: string;
  iterations: number;
  maxIterations: number;
  pendingTools: string[];
  startedAt: string;
  prompt: string;
}

export type LoopState = 
  | 'executing' 
  | 'paused' 
  | 'awaiting_approval' 
  | 'complete' 
  | 'failed' 
  | 'cancelled';

export interface Message {
  id: string;
  type: 'user' | 'assistant' | 'status' | 'error' | 'prompt';
  content: string;
  timestamp: string;
  loopId?: string;
  blocks?: MessageBlock[];
  actions?: MessageAction[];
}

export interface MessageBlock {
  type: 'text' | 'code' | 'diff';
  content: string;
  language?: string;
}

export interface MessageAction {
  id: string;
  label: string;
  signal: string;
  style: 'primary' | 'danger' | 'ghost';
}

export interface Proposal {
  id: string;
  title: string;
  description: string;
  status: ProposalStatus;
  createdBy: string;
  createdAt: string;
  spec?: Spec;
  tasks?: Task[];
}

export type ProposalStatus = 
  | 'exploring' 
  | 'drafted' 
  | 'approved' 
  | 'implementing' 
  | 'complete';

export interface Spec {
  id: string;
  proposalId: string;
  title: string;
  content: string;
  status: SpecStatus;
}

export type SpecStatus = 'draft' | 'in_review' | 'approved' | 'implemented';

export interface Task {
  id: string;
  specId: string;
  title: string;
  description: string;
  status: TaskStatus;
  loopId?: string;
}

export type TaskStatus = 'pending' | 'in_progress' | 'complete' | 'failed';

export interface Trajectory {
  loopId: string;
  steps: number;
  toolCalls: number;
  modelCalls: number;
  tokensIn: number;
  tokensOut: number;
  durationMs: number;
  entries?: TrajectoryEntry[];
}

export interface TrajectoryEntry {
  type: 'model_call' | 'model_response' | 'tool_call' | 'tool_result';
  timestamp: string;
  data: Record<string, unknown>;
}

export interface SystemHealth {
  healthy: boolean;
  components: ComponentHealth[];
  nats: NatsHealth;
}

export interface ComponentHealth {
  name: string;
  status: 'running' | 'stopped' | 'error';
  uptime: number;
  extra?: string;
}

export interface NatsHealth {
  connected: boolean;
  streamCount: number;
  streamNames: string[];
  consumerCount: number;
  messagesLastHour: number;
}

export interface CommandConfig {
  name: string;
  pattern: string;
  permission: string;
  scope: 'user' | 'system';
  category: string;
  help: string;
}
```
