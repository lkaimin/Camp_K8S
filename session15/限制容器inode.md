# 背景

之前有客户对k8s提了一些需求，需要为其定制化。有一个需求是这样的：需要分配给每个容器一定数量的inode，这个需求的背景是为了防止某一个容器过多消耗宿主机的inode资源，最终导致主机不可用。

# 技术依赖

要实现这个需求，需要使用overlay2存储驱动和xfs文件系统，下面简单对一些依赖的技术进行描述。

## inode

在Linux中，一切皆文件。文件的数据存储在block（块）中，我们需要一个地方存储文件的元信息，比如文件的大小、时间戳、block位置等等，这种存储文件元信息的区域就叫做inode。如果把blocks看作一本书的话，inode就是这本书的索引。

inode的数量是固定的（因系统而异），在分区创建完成后会确定下来；每个文件至少对应一个inode，如果文件的数量过多，把inode消耗光，则不能创建出新的文件。

```shell
# df -i
Filesystem      Inodes  IUsed   IFree IUse% Mounted on
udev           2032387    398 2031989    1% /dev
tmpfs          2037595   1224 2036371    1% /run
/dev/vda1      3276800 325180 2951620   10% /
tmpfs          2037595      7 2037588    1% /dev/shm
tmpfs          2037595      4 2037591    1% /run/lock
tmpfs          2037595     16 2037579    1% /sys/fs/cgroup
```

## xfs

在xfs文件系统上，可以使用xfs_quota工具来管理并为project控制的目录配置限额。

### Quota Types

xfs_quota支持三种类型：users、groups、projects，在本项目中利用的是xfs_quota对projects（对应docker rootfs目录）的限制。

### 前提

分区被格式化为xfs文件系统后，在mount时需要启用pquota（p也就是project）：

```shell
mount -o pquota /dev/sda1 /your_path
```

### 示例

1. 将project控制的目录添加到/etc/projects。例如，下面命令将ID为11的/var/xfs-test/test路径添加到/etc/projects（这个ID可以是任意数值）：
   
   ```shell
   echo 11:/project-path >> /etc/projects
   ```
2. 将project名称添加到/etc/projid，将project名称与ID对应起来。例如，下面命令将一个名为tests的project与上一步中定义的project ID 11关联：
   
   ```shell
   echo project-name:11 >> /etc/projid
   ```
3. 初始化project目录。例如，下面命令初始化project目录/var：
   
   ```shell
   xfs_quota -x -c 'project -s roject-name' mount-point
   ```
4. 为初始化后的project配置配额：
   
   ```shell
   xfs_quota -x -c 'limit -p ihard=1000 roject-name' mount-point
   ```

### 效果

我这里xfs文件系统分区的挂载点是/var/xfs-test

```shell
# xfs_quota
xfs_quota> df -i
Filesystem              Inodes      IUsed      IFree IUse% Pathname
/dev/sda1            244192768          5  244192763    0% /var/xfs-test
/dev/sda1                 1000          1  244192762    0% /var/xfs-test/test
```

可以看到project /var/xfs-test/test的Inodes总量已经被限制在1000了，至于IFree（空闲可用量）为什么不应该为Inodes - IUsed = 999，这个后面会分析。

## overlay2

[docker文档](https://docs.docker.com/storage/storagedriver/overlayfs-driver/)OverlayFS是一种联合文件系统(union filesystem)，速度更快且实现更简单。docker为OverlayFS提供了两种存储驱动程序：overlay和更新更稳定的overlay2。overlay2在inode利用率方便比overlay更有效，所以推荐使用更新的overlay2。

# 核心实现

docker[官方文档](https://docs.docker.com/engine/reference/commandline/run/#set-storage-driver-options-per-container)中有提到在`docker run`时，overlay2也是支持通过`--storage-opt size=10G`选项来设置容器根目录大小的，而且通过研究overlay2，可以发现这部分的工作是docker daemon在**创建读写层**的时候完成的。

这个需求是在docker 18.03.1上进行的修改，整体的调用链如下：

```
daemon.setRWLayer(container)
	--->ls.driver.CreateReadWrite(m.mountID, pid, createOpts)
		--->d.create(id, parent, opts)
			--->d.quotaCtl.SetQuota(dir, driver.options.quota)
```

`CreateReadWrite`这个接口会调用到docker具体使用的存储驱动的实现，在这个场景下（也就是overlay2），是调用到了`daemon/graphdriver/overlay2/overlay.go`中的`CreateReadWrite`函数，在`overlay.go`中进行了参数的一些校验之后，最后调用到核心函数`SetQuota`：

```c
// setProjectQuota - set the quota for project id on xfs block device
func setProjectQuota(backingFsBlockDev string, projectID uint32, quota Quota) error {
	var d C.fs_disk_quota_t
	d.d_version = C.FS_DQUOT_VERSION
	d.d_id = C.__u32(projectID)
	d.d_flags = C.XFS_PROJ_QUOTA

	d.d_fieldmask = C.FS_DQ_BHARD | C.FS_DQ_BSOFT | C.FS_DQ_IHARD | C.FS_DQ_ISOFT
	d.d_blk_hardlimit = C.__u64(quota.Size / 512)
	d.d_blk_softlimit = d.d_blk_hardlimit

	d.d_ino_hardlimit = C.__u64(quota.InodeCount)
	d.d_ino_softlimit = d.d_ino_hardlimit

	var cs = C.CString(backingFsBlockDev)
	defer C.free(unsafe.Pointer(cs))

	_, _, errno := unix.Syscall6(unix.SYS_QUOTACTL, C.Q_XSETPQLIM,
		uintptr(unsafe.Pointer(cs)), uintptr(d.d_id),
		uintptr(unsafe.Pointer(&d)), 0, 0)
	if errno != 0 {
		return fmt.Errorf("Failed to set quota limit for projid %d on %s: %v",
			projectID, backingFsBlockDev, errno.Error())
	}

	return nil
}
```

在对容器size和inode进行配置之后，调用了系统函数`quotactl`，感兴趣的话可以看一下这部分的[Linux源码](https://elixir.bootlin.com/linux/v4.10/source/fs/quota/quota.c#L835)。

## 效果

首先先编译dockerd，由于我自己主机上装的就是18.03.1的docker，就直接替换了dockerd二进制：

```shell
# docker version
Client:
 Version:      18.03.1-ce
 API version:  1.37
 Go version:   go1.9.5
 Git commit:   9ee9f40
 Built:        Thu Apr 26 07:17:20 2018
 OS/Arch:      linux/amd64
 Experimental: false
 Orchestrator: swarm

Server:
 Engine:
  Version:      dev			//忘了加version
  API version:  1.37 (minimum version 1.12)
  Go version:   go1.9.5
  Git commit:   unsupported
  Built:        Sun Jun 24 11:35:01 2022
  OS/Arch:      linux/amd64
  Experimental: false
```

编辑`/etc/docker/daemon.json`，输入以下配置：

```json
{
    "storage-opts": [
    	"overlay2.size=10G",
    	"overlay2.inode_count=1000"
  ]
}
```

创建容器，使用`df -h`和`df -i`命令查看是否生效：

<img title="" src="https://github.com/lkaimin/blog-image/blob/master/Linux/df_bug.jpg?raw=true" alt="df_bug.jpg" data-align="inline">

细心的同学可以发现`df -i`得到的结果是不正确的，跟前面是同样的问题。通过脚本在容器中不断创建文件去消耗inode，可以看到，limit是起了作用的：

![df_bug_expend.jpg](https://github.com/lkaimin/blog-image/blob/master/Linux/df_bug_expend.jpg?raw=true)

## 问题分析

既然`df -i`得到的结果有问题，那就去分析df的[源码](https://github.com/coreutils/gnulib/blob/48a6c46b/lib/fsusage.c#L112)，df调用的是内核中的`statfs()`函数（`/fs/statfs.c`），继续往下走，在xfs文件系统中调用的是`xfs_fs_statfs()`函数（`/fs/xfs/xfs_super.c`），调用栈如下：

```
xfs_fs_statfs()
	--->xfs_qm_statvfs()
		--->xfs_qm_dqget()
			--->xfs_fill_statvfs_from_dquot()
```

下面详细看一下`xfs_fill_statvfs_from_dquot()`函数：

![xfs_fill_statvfs_from_dquot.jpg](https://github.com/lkaimin/blog-image/blob/master/Linux/xfs_fill_statvfs_from_dquot.jpg?raw=true)

入参statp保存的是`xfs_fs_statfs()`函数拿到的superblock（超级块）的block和inode使用量信息，入参dqp保存的是`xfs_qm_dqget()`函数拿到的对应project（也就是容器根目录）的block和inode使用量信息。

为什么`df -h`得到的block信息是正常的呢？仔细看一下f_bfree（block空闲值）的计算方式，再看一下f_ffree（inode空闲值）的计算方式，可以发现，f_ffree的计算方式是错误的，应该用f_files（inode总量）减去使用量：

```c
if (limit && statp->f_files > limit) {
		statp->f_files = limit;
		statp->f_ffree =
			(statp->f_files > dqp->q_res_icount) ?
			 (statp->f_files - dqp->q_res_icount) : 0;
}
```

这个bug在内核**4.19.21**中得到修复，升级内核版本后就ok啦。
