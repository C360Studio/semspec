const GRAPHQL_URL = '/graphql';

export interface GraphQLError {
	message: string;
	locations?: { line: number; column: number }[];
	path?: (string | number)[];
}

export interface GraphQLResponse<T> {
	data?: T;
	errors?: GraphQLError[];
}

export async function graphqlRequest<T>(
	query: string,
	variables?: Record<string, unknown>
): Promise<T> {
	const response = await fetch(GRAPHQL_URL, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ query, variables })
	});

	if (!response.ok) {
		throw new Error(`GraphQL request failed: ${response.status}`);
	}

	const json: GraphQLResponse<T> = await response.json();
	if (json.errors?.length) {
		throw new Error(json.errors[0].message);
	}

	if (!json.data) {
		throw new Error('No data in GraphQL response');
	}

	// Detect JetStream PubAck leaked through graph-gateway routing
	const values = Object.values(json.data);
	if (
		values.length === 1 &&
		values[0] &&
		typeof values[0] === 'object' &&
		'stream' in values[0] &&
		'seq' in values[0]
	) {
		throw new Error('Graph query returned PubAck instead of data — check graph-gateway routing');
	}

	return json.data;
}
