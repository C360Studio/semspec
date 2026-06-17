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
		'git-merge': icons.GitMerge,
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
		'file-plus': icons.FilePlus,
		'book-open': icons.BookOpen,
		code: icons.Code,
		folder: icons.Folder,
		hammer: icons.Hammer,
		'test-tube': icons.TestTube,
		upload: icons.Upload,
		'upload-cloud': icons.UploadCloud,
		trash: icons.Trash2,
		'trash-2': icons.Trash2,
		download: icons.Download,
		globe: icons.Globe,
		link: icons.Link,
		unlink: icons.Unlink,
		layers: icons.Layers,
		search: icons.Search,
		'maximize-2': icons.Maximize2,
		list: icons.List,
		menu: icons.Menu,
		lock: icons.Lock,
		archive: icons.Archive,
		eye: icons.Eye,
		'x-circle': icons.XCircle,
		'play-circle': icons.PlayCircle,
		'check-square': icons.CheckSquare,
		lightbulb: icons.Lightbulb,
		'arrow-left': icons.ArrowLeft,
		scissors: icons.Scissors,
		'skip-forward': icons.SkipForward,
		'arrow-right-circle': icons.ArrowRightCircle,
		pencil: icons.Pencil,
		'pencil-line': icons.PencilLine,
		'minus-circle': icons.MinusCircle,
		'rotate-ccw': icons.RotateCcw,
		'arrow-up': icons.ArrowUp,
		'arrow-down': icons.ArrowDown,
		'wifi-off': icons.WifiOff,
		columns: icons.Columns3,
		users: icons.Users
	};

	const IconComponent = $derived(iconMap[name] || icons.Circle);
</script>

{#if IconComponent}
	<IconComponent {size} {strokeWidth} class={className} />
{/if}
