#!/usr/bin/env python3
"""
Dockerfile Dependency Analysis Tool

This script analyzes a multi-stage Dockerfile to:
1. Extract build stage dependencies
2. Generate a dependency graph visualization
3. Validate build order using topological sort
4. Compare against a checked-in "facit" (source of truth)
5. Output results suitable for GitHub Actions summaries

Usage:
    # Generate facit file
    python3 dockerfile-deps.py --generate-facit

    # Validate against facit
    python3 dockerfile-deps.py --validate

    # Just show the analysis
    python3 dockerfile-deps.py
"""

import argparse
import json
import re
import sys
from collections import defaultdict, deque
from pathlib import Path
from typing import Dict, List, Set, Tuple


class DockerfileAnalyzer:
    def __init__(self, dockerfile_path: str):
        self.dockerfile_path = Path(dockerfile_path)
        self.stages: Dict[str, str] = {}  # stage_name -> base_image
        self.dependencies: Dict[str, Set[str]] = defaultdict(set)  # stage -> depends_on_stages
        self.file_dependencies: Dict[str, Set[str]] = defaultdict(set)  # stage -> copied_files

    def parse(self):
        """Parse Dockerfile and extract stages and dependencies"""
        with open(self.dockerfile_path) as f:
            content = f.read()

        # Extract FROM statements with stage names
        # Matches: FROM <image> AS <stage>
        from_pattern = re.compile(
            r'^\s*FROM\s+(?P<base>\S+)(?:\s+AS\s+(?P<stage>\S+))?',
            re.MULTILINE | re.IGNORECASE
        )

        # Extract COPY --from statements
        # Matches: COPY --from=<stage> <src> <dest>
        copy_from_pattern = re.compile(
            r'COPY\s+--from=(?P<stage>\S+)\s+(?P<src>\S+)',
            re.IGNORECASE
        )

        # Extract regular COPY/ADD statements (from build context)
        # Matches: COPY <src> <dest> or ADD <src> <dest>
        # But NOT COPY --from=...
        copy_pattern = re.compile(
            r'^\s*(?:COPY|ADD)\s+(?!--from=)(?P<src>\S+)',
            re.MULTILINE | re.IGNORECASE
        )

        current_stage = None
        for match in from_pattern.finditer(content):
            base = match.group('base')
            stage = match.group('stage')

            if stage:
                current_stage = stage
                self.stages[stage] = base

                # Check if base is another stage
                if base in self.stages:
                    self.dependencies[stage].add(base)

        # Find COPY --from and regular COPY dependencies
        lines = content.split('\n')
        current_stage = None

        for line in lines:
            # Track current stage
            from_match = from_pattern.match(line)
            if from_match and from_match.group('stage'):
                current_stage = from_match.group('stage')

            if not current_stage:
                continue

            # Find COPY --from references (stage dependencies)
            copy_from_match = copy_from_pattern.search(line)
            if copy_from_match:
                dep_stage = copy_from_match.group('stage')
                if dep_stage in self.stages:
                    self.dependencies[current_stage].add(dep_stage)

            # Find regular COPY/ADD (file dependencies)
            copy_match = copy_pattern.match(line)
            if copy_match:
                src = copy_match.group('src')
                # Skip URLs and ignore special Docker build args
                if not src.startswith('http') and not src.startswith('--'):
                    self.file_dependencies[current_stage].add(src)

    def topological_sort(self) -> Tuple[List[str], bool]:
        """
        Perform topological sort on the dependency graph.
        Returns (sorted_stages, has_cycle)

        In-degree = number of dependencies pointing TO this node
        A node with in-degree 0 has no dependencies and can be processed first
        """
        # Calculate in-degrees (number of dependencies each stage has)
        in_degree = {stage: len(self.dependencies.get(stage, set())) for stage in self.stages}

        # Find all stages with no dependencies (in-degree 0)
        queue = deque([stage for stage, degree in in_degree.items() if degree == 0])
        sorted_stages = []

        while queue:
            stage = queue.popleft()
            sorted_stages.append(stage)

            # For each other stage, check if it depends on the current stage
            # If so, decrement its in-degree (one dependency satisfied)
            for other_stage in self.stages:
                if stage in self.dependencies.get(other_stage, set()):
                    in_degree[other_stage] -= 1
                    if in_degree[other_stage] == 0:
                        queue.append(other_stage)

        has_cycle = len(sorted_stages) != len(self.stages)
        return sorted_stages, has_cycle

    def validate_dependencies(self) -> Tuple[bool, List[str]]:
        """Validate that the dependency graph has no cycles"""
        sorted_stages, has_cycle = self.topological_sort()
        errors = []

        if has_cycle:
            errors.append("❌ Cycle detected in dependency graph!")
            missing = set(self.stages.keys()) - set(sorted_stages)
            errors.append(f"   Stages involved in cycle: {', '.join(missing)}")

        return not has_cycle, errors

    def to_dict(self) -> dict:
        """Export dependencies as a dictionary for serialization"""
        return {
            "stages": self.stages,
            "dependencies": {k: sorted(list(v)) for k, v in self.dependencies.items()},
            "file_dependencies": {k: sorted(list(v)) for k, v in self.file_dependencies.items()}
        }

    def compare_with_facit(self, facit_path: Path) -> Tuple[bool, List[str]]:
        """Compare current dependencies with facit file"""
        if not facit_path.exists():
            return False, [f"❌ Facit file not found: {facit_path}"]

        with open(facit_path) as f:
            facit = json.load(f)

        errors = []

        # Compare stages
        facit_stages = set(facit.get("stages", {}).keys())
        current_stages = set(self.stages.keys())

        if facit_stages != current_stages:
            added = current_stages - facit_stages
            removed = facit_stages - current_stages

            if added:
                errors.append(f"❌ New stages added: {', '.join(sorted(added))}")
            if removed:
                errors.append(f"❌ Stages removed: {', '.join(sorted(removed))}")

        # Compare stage dependencies for common stages
        for stage in facit_stages & current_stages:
            facit_deps = set(facit.get("dependencies", {}).get(stage, []))
            current_deps = self.dependencies.get(stage, set())

            if facit_deps != current_deps:
                added_deps = current_deps - facit_deps
                removed_deps = facit_deps - current_deps

                if added_deps or removed_deps:
                    errors.append(f"❌ Stage dependencies changed for '{stage}':")
                    if added_deps:
                        errors.append(f"   Added stages: {', '.join(sorted(added_deps))}")
                    if removed_deps:
                        errors.append(f"   Removed stages: {', '.join(sorted(removed_deps))}")

        # Compare file dependencies for common stages
        for stage in facit_stages & current_stages:
            facit_files = set(facit.get("file_dependencies", {}).get(stage, []))
            current_files = self.file_dependencies.get(stage, set())

            if facit_files != current_files:
                added_files = current_files - facit_files
                removed_files = facit_files - current_files

                if added_files or removed_files:
                    errors.append(f"❌ File dependencies changed for '{stage}':")
                    if added_files:
                        errors.append(f"   Added files: {', '.join(sorted(added_files))}")
                    if removed_files:
                        errors.append(f"   Removed files: {', '.join(sorted(removed_files))}")

        return len(errors) == 0, errors

    def generate_mermaid(self) -> str:
        """Generate Mermaid flowchart from dependencies"""
        lines = ["graph TD"]

        # Add nodes (exclude "all" stage as it's just a collection target)
        for stage in self.stages:
            if stage == "all":
                continue
            # Sanitize stage names for Mermaid
            node_id = stage.replace("-", "_").replace(".", "_")
            lines.append(f"    {node_id}[\"{stage}\"]")

        # Add edges (dependencies) - skip anything involving "all"
        for stage, deps in self.dependencies.items():
            if stage == "all":
                continue
            stage_id = stage.replace("-", "_").replace(".", "_")
            for dep in deps:
                if dep == "all":
                    continue
                dep_id = dep.replace("-", "_").replace(".", "_")
                lines.append(f"    {dep_id} --> {stage_id}")

        return "\n".join(lines)

    def generate_report(self, facit_path: Path = None) -> str:
        """Generate a human-readable report"""
        lines = []
        lines.append("# Dockerfile Dependency Analysis")
        lines.append("")
        lines.append(f"**Dockerfile:** `{self.dockerfile_path}`")
        lines.append(f"**Total Stages:** {len(self.stages)}")
        lines.append("")

        # Facit validation
        if facit_path:
            is_valid, errors = self.compare_with_facit(facit_path)
            if is_valid:
                lines.append("## Facit Validation: ✅ PASS")
                lines.append("")
                lines.append(f"Dependencies match the expected structure in `{facit_path}`")
            else:
                lines.append("## Facit Validation: ❌ FAIL")
                lines.append("")
                for error in errors:
                    lines.append(error)
                lines.append("")
                lines.append("**Action required:** Update the facit file or fix the Dockerfile:")
                lines.append("")
                lines.append("```bash")
                lines.append("# If changes are intentional, update facit:")
                lines.append("python3 tools/dockerfile-deps.py --generate-facit")
                lines.append("")
                lines.append("# Then commit the updated facit:")
                lines.append("git add .dockerfile-deps.json")
                lines.append("git commit -m 'chore: update dockerfile dependencies facit'")
                lines.append("```")
            lines.append("")

        # Topological validation
        is_valid, errors = self.validate_dependencies()
        if is_valid:
            lines.append("## Topological Validation: ✅ PASS")
            lines.append("")
            lines.append("No circular dependencies detected.")
        else:
            lines.append("## Topological Validation: ❌ FAIL")
            lines.append("")
            for error in errors:
                lines.append(error)

        lines.append("")

        # Topological order
        sorted_stages, has_cycle = self.topological_sort()
        if not has_cycle:
            lines.append("## Build Order (Topological Sort)")
            lines.append("")
            lines.append("Stages should be built in this order:")
            lines.append("")
            for i, stage in enumerate(sorted_stages, 1):
                deps = self.dependencies.get(stage, set())
                files = self.file_dependencies.get(stage, set())

                dep_parts = []
                if deps:
                    dep_parts.append(f"stages: {', '.join(sorted(deps))}")
                if files:
                    file_list = ', '.join(sorted(files))
                    if len(file_list) > 80:
                        file_count = len(files)
                        file_list = f"{file_count} files"
                    dep_parts.append(f"files: {file_list}")

                if dep_parts:
                    dep_str = f" (depends on: {'; '.join(dep_parts)})"
                else:
                    dep_str = ""
                lines.append(f"{i}. `{stage}`{dep_str}")

        lines.append("")
        lines.append("## Dependency Graph")
        lines.append("")
        lines.append("```mermaid")
        lines.append(self.generate_mermaid())
        lines.append("```")
        lines.append("")

        return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(
        description="Analyze Dockerfile dependencies and validate against facit"
    )
    parser.add_argument(
        "--dockerfile",
        default="Dockerfile",
        help="Path to Dockerfile (default: Dockerfile)"
    )
    parser.add_argument(
        "--facit",
        default=".dockerfile-deps.json",
        help="Path to facit file (default: .dockerfile-deps.json)"
    )
    parser.add_argument(
        "--generate-facit",
        action="store_true",
        help="Generate facit file from current Dockerfile"
    )
    parser.add_argument(
        "--validate",
        action="store_true",
        help="Validate Dockerfile against facit"
    )
    parser.add_argument(
        "--mermaid-only",
        action="store_true",
        help="Output only the Mermaid diagram (for CI summaries)"
    )

    args = parser.parse_args()

    dockerfile = Path(args.dockerfile)
    facit_path = Path(args.facit)

    if not dockerfile.exists():
        print(f"Error: {dockerfile} not found", file=sys.stderr)
        sys.exit(1)

    analyzer = DockerfileAnalyzer(str(dockerfile))
    analyzer.parse()

    if args.generate_facit:
        # Generate and save facit
        with open(facit_path, 'w') as f:
            json.dump(analyzer.to_dict(), f, indent=2, sort_keys=True)
        print(f"✅ Generated facit file: {facit_path}")
        sys.exit(0)

    if args.mermaid_only:
        # Output only Mermaid diagram wrapped in code fence
        print("```mermaid")
        print(analyzer.generate_mermaid())
        print("```")
        sys.exit(0)

    # Generate report
    if args.validate:
        report = analyzer.generate_report(facit_path)
        print(report)

        # Exit with error if validation failed
        facit_valid, _ = analyzer.compare_with_facit(facit_path)
        topo_valid, _ = analyzer.validate_dependencies()
        sys.exit(0 if (facit_valid and topo_valid) else 1)
    else:
        report = analyzer.generate_report()
        print(report)

        # Exit with error if topological validation failed
        is_valid, _ = analyzer.validate_dependencies()
        sys.exit(0 if is_valid else 1)


if __name__ == "__main__":
    main()
