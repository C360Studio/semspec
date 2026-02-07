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
		'alert-circle': icons.AlertCircle
	};

	const IconComponent = $derived(iconMap[name] || icons.Circle);
</script>

{#if IconComponent}
	<IconComponent {size} {strokeWidth} class={className} />
{/if}
