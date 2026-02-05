import type { Entity, EntityType, Relationship } from '$lib/types';

/**
 * Raw entity format from graph-query GraphQL responses.
 */
export interface RawTriple {
	subject: string;
	predicate: string;
	object: unknown;
}

export interface RawEntity {
	id: string;
	triples: RawTriple[];
}

export interface RawRelationship {
	from: string;
	to: string;
	predicate: string;
	direction: 'outgoing' | 'incoming';
}

export interface HierarchyChild {
	name: string;
	count: number;
}

export interface EntityIdHierarchy {
	children: HierarchyChild[];
	totalEntities: number;
}

/**
 * Extract entity type from ID prefix.
 * Example: "code.file.main-go" → "code"
 */
function extractTypeFromId(id: string): EntityType {
	const firstDot = id.indexOf('.');
	if (firstDot === -1) return 'code';

	const prefix = id.substring(0, firstDot);
	const validTypes: EntityType[] = ['code', 'proposal', 'spec', 'task', 'loop', 'activity'];
	return validTypes.includes(prefix as EntityType) ? (prefix as EntityType) : 'code';
}

/**
 * Transform a raw entity from graph-query format to UI Entity format.
 */
export function transformEntity(raw: RawEntity): Entity {
	const predicates: Record<string, unknown> = {};
	let name = raw.id;
	let createdAt: string | undefined;
	let updatedAt: string | undefined;

	for (const triple of raw.triples) {
		predicates[triple.predicate] = triple.object;

		// Extract display name from common predicates
		if (triple.predicate === 'dc.terms.title') {
			name = triple.object as string;
		} else if (triple.predicate === 'code.artifact.path') {
			// For code entities, use filename from path as name
			const path = triple.object as string;
			const filename = path.split('/').pop();
			if (filename && name === raw.id) {
				name = filename;
			}
		} else if (triple.predicate === 'prov.generatedAtTime') {
			createdAt = triple.object as string;
		} else if (triple.predicate === 'prov.invalidatedAtTime') {
			updatedAt = triple.object as string;
		}
	}

	const type = extractTypeFromId(raw.id);

	return {
		id: raw.id,
		type,
		name,
		predicates,
		...(createdAt && { createdAt }),
		...(updatedAt && { updatedAt })
	};
}

/**
 * Transform raw relationships from graph-query to UI Relationship format.
 */
export function transformRelationships(raw: RawRelationship[]): Relationship[] {
	return raw.map((r) => {
		const targetId = r.direction === 'outgoing' ? r.to : r.from;

		// Extract a human-readable label from the predicate
		// e.g., "code.structure.contains" → "contains"
		const predicateParts = r.predicate.split('.');
		const predicateLabel = predicateParts[predicateParts.length - 1] || r.predicate;

		return {
			predicate: r.predicate,
			predicateLabel,
			targetId,
			targetType: extractTypeFromId(targetId),
			targetName: targetId, // Would need another query to get actual name
			direction: r.direction
		};
	});
}

/**
 * Transform entity hierarchy counts to the format expected by the UI.
 */
export function transformEntityCounts(hierarchy: EntityIdHierarchy): {
	total: number;
	byType: Record<string, number>;
} {
	const byType: Record<string, number> = {};
	for (const child of hierarchy.children) {
		byType[child.name] = child.count;
	}
	return {
		total: hierarchy.totalEntities,
		byType
	};
}
