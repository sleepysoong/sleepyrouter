"""NVIDIA provider adapter."""

from .base import BaseProviderAdapter


class NVIDIAProviderAdapter(BaseProviderAdapter):
    def __init__(self) -> None:
        super().__init__(
            name="NVIDIA",
            source="nvidia",
            api_key_env_var="NVIDIA_API_KEY",
            api_base="https://integrate.api.nvidia.com/v1",
        )
