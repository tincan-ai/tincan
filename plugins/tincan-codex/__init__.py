"""Hermes platform entry point; keep imports deferred until gateway activation."""
def register(ctx):
    from .native.hermes.adapter import register as register_platform
    register_platform(ctx)
