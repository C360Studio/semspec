<script lang="ts">
	import * as icons from 'lucide-svelte';

	interface Props {
		name: string;
		size?: number;
		strokeWidth?: number;
		class?: string;
	}

	let { name, size = 24, strokeWidth = 2, class: className = '' }: Props = $props();

	// Map icon names to lucide components
	const iconMap: Record<string, typeof icons.Activity> = {
		activity: icons.Activity,
		'chevron-down': icons.ChevronDown,
		'chevron-up': icons.ChevronUp,
		'chevron-left': icons.ChevronLeft,
		'chevron-right': icons.ChevronRight,
		check: icons.Check,
		'check-circle': icons.CheckCircle,
		x: icons.X,
		send: icons.Send,
		loader: icons.Loader2,
		'message-square': icons.MessageSquare,
		'layout-dashboard': icons.LayoutDashboard,
		'layout-grid': icons.LayoutGrid,
		'git-pull-request': icons.GitPullRequest,
		'git-branch': icons.GitBranch,
		'git-commit': icons.GitCommit,
		'list-checks': icons.ListChecks,
		history: icons.History,
		settings: icons.Settings,
		circle: icons.Circle,
		play: icons.Play,
		pause: icons.Pause,
		clock: icons.Clock,
		slash: icons.Slash,
		user: icons.User,
		bot: icons.Bot,
		brain: icons.Brain,
		cpu: icons.Cpu,
		database: icons.Database,
		inbox: icons.Inbox,
		wrench: icons.Wrench,
		'alert-circle': icons.AlertCircle,
		'alert-triangle': icons.AlertTriangle,
		'help-circle': icons.HelpCircle,
		tag: icons.Tag,
		'edit-3': icons.Edit3,
		'external-link': icons.ExternalLink,
		'refresh-cw': icons.RefreshCw,
		plus: icons.Plus,
		info: icons.Info,
		file: icons.File,
		'file-text': icons.FileText,
		'book-open': icons.BookOpen,
		code: icons.Code,
		folder: icons.Folder,
		upload: icons.Upload,
		trash: icons.Trash2,
		download: icons.Download
	};

	const IconComponent = $derived(iconMap[name] || icons.Circle);
</script>

{#if IconComponent}
	<IconComponent {size} {strokeWidth} class={className} />
{/if}
