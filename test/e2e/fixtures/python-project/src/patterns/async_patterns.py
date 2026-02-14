"""Async/await patterns and utilities."""

import asyncio
from typing import (
    TypeVar, Generic, Optional, List, Callable, Awaitable,
    AsyncIterator, Any, Dict
)
from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum, auto
import logging


logger = logging.getLogger(__name__)


T = TypeVar('T')
R = TypeVar('R')


class TaskStatus(Enum):
    """Status of an async task."""
    PENDING = auto()
    RUNNING = auto()
    COMPLETED = auto()
    FAILED = auto()
    CANCELLED = auto()


@dataclass
class TaskResult(Generic[T]):
    """Result of an async task execution.

    Attributes:
        task_id: Unique identifier for the task
        status: Current status of the task
        result: The result value (if completed successfully)
        error: Error message (if failed)
        started_at: When the task started
        completed_at: When the task finished
    """
    task_id: str
    status: TaskStatus
    result: Optional[T] = None
    error: Optional[str] = None
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None

    @property
    def duration_ms(self) -> Optional[float]:
        """Calculate task duration in milliseconds."""
        if self.started_at and self.completed_at:
            return (self.completed_at - self.started_at).total_seconds() * 1000
        return None


class AsyncWorker(Generic[T, R]):
    """A worker that processes items asynchronously.

    Provides controlled concurrency with configurable
    parallelism and error handling.

    Example:
        async def process_url(url: str) -> dict:
            async with aiohttp.ClientSession() as session:
                async with session.get(url) as resp:
                    return await resp.json()

        worker = AsyncWorker(process_url, max_concurrent=5)
        results = await worker.process_all(urls)
    """

    def __init__(
        self,
        processor: Callable[[T], Awaitable[R]],
        max_concurrent: int = 10,
        timeout: Optional[float] = None
    ):
        """Initialize the async worker.

        Args:
            processor: Async function to process each item
            max_concurrent: Maximum concurrent tasks
            timeout: Optional timeout per task in seconds
        """
        self._processor = processor
        self._max_concurrent = max_concurrent
        self._timeout = timeout
        self._semaphore = asyncio.Semaphore(max_concurrent)

    async def process_one(self, item: T) -> TaskResult[R]:
        """Process a single item.

        Args:
            item: The item to process

        Returns:
            TaskResult containing the result or error
        """
        task_id = str(id(item))
        result = TaskResult[R](
            task_id=task_id,
            status=TaskStatus.PENDING
        )

        async with self._semaphore:
            result.started_at = datetime.utcnow()
            result.status = TaskStatus.RUNNING

            try:
                if self._timeout:
                    processed = await asyncio.wait_for(
                        self._processor(item),
                        timeout=self._timeout
                    )
                else:
                    processed = await self._processor(item)

                result.result = processed
                result.status = TaskStatus.COMPLETED

            except asyncio.CancelledError:
                result.status = TaskStatus.CANCELLED
                result.error = "Task was cancelled"

            except asyncio.TimeoutError:
                result.status = TaskStatus.FAILED
                result.error = f"Task timed out after {self._timeout}s"

            except Exception as e:
                result.status = TaskStatus.FAILED
                result.error = str(e)
                logger.exception(f"Task {task_id} failed: {e}")

            finally:
                result.completed_at = datetime.utcnow()

        return result

    async def process_all(self, items: List[T]) -> List[TaskResult[R]]:
        """Process all items concurrently.

        Args:
            items: List of items to process

        Returns:
            List of TaskResults in the same order as input
        """
        tasks = [self.process_one(item) for item in items]
        return await asyncio.gather(*tasks)

    async def process_stream(self, items: List[T]) -> AsyncIterator[TaskResult[R]]:
        """Process items and yield results as they complete.

        Args:
            items: List of items to process

        Yields:
            TaskResults as each task completes (unordered)
        """
        tasks = {asyncio.create_task(self.process_one(item)): item for item in items}

        for completed in asyncio.as_completed(tasks.keys()):
            yield await completed


@dataclass
class QueuedTask(Generic[T]):
    """A task in the queue with metadata."""
    id: str
    payload: T
    priority: int = 0
    enqueued_at: datetime = field(default_factory=datetime.utcnow)
    retries: int = 0
    max_retries: int = 3


class TaskQueue(Generic[T, R]):
    """An async task queue with priority support.

    Provides a producer-consumer pattern with configurable
    concurrency and retry handling.
    """

    def __init__(
        self,
        processor: Callable[[T], Awaitable[R]],
        max_workers: int = 5,
        max_queue_size: int = 1000
    ):
        """Initialize the task queue.

        Args:
            processor: Async function to process each task
            max_workers: Number of concurrent workers
            max_queue_size: Maximum queue size (0 for unlimited)
        """
        self._processor = processor
        self._max_workers = max_workers
        self._queue: asyncio.PriorityQueue[tuple] = asyncio.PriorityQueue(
            maxsize=max_queue_size
        )
        self._results: Dict[str, TaskResult[R]] = {}
        self._running = False
        self._workers: List[asyncio.Task] = []

    async def enqueue(
        self,
        task_id: str,
        payload: T,
        priority: int = 0
    ) -> None:
        """Add a task to the queue.

        Args:
            task_id: Unique identifier for the task
            payload: The data to process
            priority: Lower number = higher priority
        """
        task = QueuedTask(id=task_id, payload=payload, priority=priority)
        # Use negative priority for min-heap behavior
        await self._queue.put((-priority, task.enqueued_at, task))
        logger.debug(f"Enqueued task {task_id} with priority {priority}")

    async def start(self) -> None:
        """Start the worker tasks."""
        if self._running:
            return

        self._running = True
        for i in range(self._max_workers):
            worker = asyncio.create_task(self._worker(i))
            self._workers.append(worker)

        logger.info(f"Started {self._max_workers} workers")

    async def stop(self, wait: bool = True) -> None:
        """Stop the worker tasks.

        Args:
            wait: If True, wait for queue to drain before stopping
        """
        if wait:
            await self._queue.join()

        self._running = False
        for worker in self._workers:
            worker.cancel()

        await asyncio.gather(*self._workers, return_exceptions=True)
        self._workers.clear()
        logger.info("Task queue stopped")

    async def get_result(self, task_id: str) -> Optional[TaskResult[R]]:
        """Get the result of a completed task.

        Args:
            task_id: The task identifier

        Returns:
            TaskResult if available, None otherwise
        """
        return self._results.get(task_id)

    @property
    def queue_size(self) -> int:
        """Current number of pending tasks."""
        return self._queue.qsize()

    async def _worker(self, worker_id: int) -> None:
        """Worker coroutine that processes tasks from the queue."""
        logger.debug(f"Worker {worker_id} started")

        while self._running:
            try:
                # Get task with timeout to allow checking _running flag
                try:
                    _, _, task = await asyncio.wait_for(
                        self._queue.get(),
                        timeout=1.0
                    )
                except asyncio.TimeoutError:
                    continue

                result = TaskResult[R](
                    task_id=task.id,
                    status=TaskStatus.RUNNING,
                    started_at=datetime.utcnow()
                )

                try:
                    processed = await self._processor(task.payload)
                    result.result = processed
                    result.status = TaskStatus.COMPLETED

                except Exception as e:
                    if task.retries < task.max_retries:
                        # Re-queue for retry
                        task.retries += 1
                        await self._queue.put((-task.priority, task.enqueued_at, task))
                        logger.warning(
                            f"Task {task.id} failed, retry {task.retries}/{task.max_retries}"
                        )
                        continue
                    else:
                        result.status = TaskStatus.FAILED
                        result.error = str(e)

                finally:
                    result.completed_at = datetime.utcnow()
                    self._results[task.id] = result
                    self._queue.task_done()

            except asyncio.CancelledError:
                break

        logger.debug(f"Worker {worker_id} stopped")


async def gather_with_limit(
    coros: List[Awaitable[T]],
    limit: int
) -> List[T]:
    """Run coroutines with a concurrency limit.

    Args:
        coros: List of coroutines to run
        limit: Maximum concurrent coroutines

    Returns:
        List of results in the same order as input
    """
    semaphore = asyncio.Semaphore(limit)

    async def limited_coro(coro: Awaitable[T]) -> T:
        async with semaphore:
            return await coro

    return await asyncio.gather(*[limited_coro(c) for c in coros])
