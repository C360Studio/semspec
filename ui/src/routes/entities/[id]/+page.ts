import type { PageLoad } from './$types';
import type { EntityWithRelationships } from '$lib/types';
import { transformEntity, transformRelationships, type RawEntity, type RawRelationship } from '$lib/api/transforms';

export const load: PageLoad = async ({ params, fetch }) => {
	const id = decodeURIComponent(params.id);

	try {
		const response = await fetch('/graphql', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				query: `
					query($id: String!) {
						entity(id: $id) {
							id
							triples { subject predicate object }
						}
						relationships(entityId: $id) {
							from
							to
							predicate
							direction
						}
					}
				`,
				variables: { id }
			})
		});

		if (!response.ok) {
			return { entity: null, error: `Failed to load entity: ${response.status}` };
		}

		const json = await response.json();
		if (json.errors?.length) {
			return { entity: null, error: json.errors[0].message };
		}

		const raw: { entity: RawEntity; relationships: RawRelationship[] } = json.data;
		if (!raw.entity) {
			return { entity: null, error: 'Entity not found' };
		}

		const entity: EntityWithRelationships = {
			...transformEntity(raw.entity),
			relationships: transformRelationships(raw.relationships || [])
		};

		return { entity, error: null };
	} catch (e) {
		return { entity: null, error: e instanceof Error ? e.message : 'Failed to load entity' };
	}
};
